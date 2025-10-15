import torch
import json
import numpy as np
from flask import Flask, request, jsonify
from transformers import BertConfig, BertModel, BertTokenizer

app = Flask(__name__)

config = BertConfig.from_json_file('model/config.json')
bert = BertModel.from_pretrained('model', config=config)
tokenizer = BertTokenizer.from_pretrained('model/vocab.txt', do_lower_case=True)

lr_model = torch.load('ranking_model/LTRtinyBertL2H128.pt', weights_only=False)
print("модель линейной регрессии загружена")

class X_data:
    def __init__(self, cos, euclid_dist, sum_token_in_package, words_in_header, query_coverage, query_dencity, term_proximity):
        self.cos = cos
        self.euclid_dist = euclid_dist
        self.sum_token_in_package = sum_token_in_package
        self.words_in_header = words_in_header
        self.query_coverage = query_coverage
        self.query_dencity = query_dencity
        self.term_proximity = term_proximity

class Document:
    def __init__(self, text: str):
        self.text = text

def get_cls_embeddings(content: str, max_length=512) -> list[list[float]]:
    enc = tokenizer(
        content,
        return_tensors="pt",
        max_length=max_length,
        truncation=True,
        return_overflowing_tokens=True,
        stride=50,
    )
    input_ids = enc["input_ids"]
    attention_mask = enc["attention_mask"]
    token_type_ids = enc.get("token_type_ids")

    with torch.no_grad():
        if token_type_ids is not None:
            outputs = bert(
                input_ids=input_ids,
                attention_mask=attention_mask,
                token_type_ids=token_type_ids,
            )
        else:
            outputs = bert(
                input_ids=input_ids,
                attention_mask=attention_mask,
            )
        cls_embeddings = outputs.last_hidden_state[:, 0, :].cpu().numpy()

    return cls_embeddings.tolist()

@app.route('/vectorize', methods=['POST'])
def get_embeddings():
    doc_data = request.get_json()
    if not doc_data or 'text' not in doc_data:
        return jsonify({'error': 'Invalid input'}), 400

    doc = Document(text=doc_data['text'])
    matrix_vec = get_cls_embeddings(doc.text)
    return jsonify({'vec': matrix_vec})

@app.route('/rank', methods=['POST'])
def get_ranked():
    request_data = request.get_json()
    x_data = []
    for entry in json.load(request_data):
        x_data.append(X_data(entry))

    resp = lr_model.predict(np.array(x_data))
    return jsonify({'rel': resp.tolist()})


# Run the Flask app
if __name__ == '__main__':
    app.run(debug=True, port=50920)