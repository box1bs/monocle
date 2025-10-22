import torch
import numpy as np
from flask import Flask, request, jsonify
from transformers import BertConfig, BertModel, BertTokenizer

app = Flask(__name__)

MODEL_DIR = 'model'
RANKING_MODEL_PATH = 'ranking_model/LTRtinyBertL2H128.pt'

config = BertConfig.from_json_file(f'{MODEL_DIR}/config.json')
bert = BertModel.from_pretrained(MODEL_DIR, config=config)
tokenizer = BertTokenizer.from_pretrained(f'{MODEL_DIR}/vocab.txt', do_lower_case=True)

lr_model = torch.load(RANKING_MODEL_PATH, weights_only=False)

class X_data:
    def __init__(self, cos, euclid_dist, sum_token_in_package, words_in_header, query_coverage, query_dencity, term_proximity):
        self.cos = cos
        self.euclid_dist = euclid_dist
        self.sum_token_in_package = sum_token_in_package
        self.words_in_header = words_in_header
        self.query_coverage = query_coverage
        self.query_dencity = query_dencity
        self.term_proximity = term_proximity

    @classmethod
    def from_dict(self, d: dict):
        return self(
            cos=d.get("cos", 0.0),
            euclid_dist=d.get("euclid_dist", 0.0),
            sum_token_in_package=d.get("sum_token_in_package", 0),
            words_in_header=d.get("words_in_header", 0),
            query_coverage=d.get("query_coverage", 0.0),
            query_dencity=d.get("query_dencity", 0.0),
            term_proximity=d.get("term_proximity", 0),
        )

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
    if not doc_data:
        return jsonify({'error': 'Invalid input'}), 400

    doc = [Document(text=d['text']) for d in doc_data]
    return jsonify({'vec': [get_cls_embeddings(d.text) for d in doc]})

@app.route('/rank', methods=['POST'])
def get_ranked():
    request_data = request.get_json()
    features = []
    for entry in request_data:
        print(entry)
        x = X_data.from_dict(entry)
        features.append([
            x.cos,
            x.euclid_dist,
            x.sum_token_in_package,
            x.words_in_header,
            x.query_coverage,
            x.query_dencity,
            x.term_proximity,
        ])

    try:
        resp = lr_model.predict(np.array(features, dtype=float))
    except Exception as e:
        return jsonify({'error': f'prediction error: {e}'}), 500

    return jsonify({'rel': np.asarray(resp).tolist()})

if __name__ == '__main__':
    app.run(debug=True, port=50920)