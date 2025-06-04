import torch
from flask import Flask, request, jsonify
from transformers import BertConfig, BertModel, BertTokenizer
from summarizer import Summarizer

summarizer = Summarizer()

app = Flask(__name__)

config = BertConfig.from_json_file('model/config.json')
model = BertModel.from_pretrained('model', config=config)
tokenizer = BertTokenizer.from_pretrained('model/vocab.txt', do_lower_case=True)

class Document:
    text: str
    def __init__(self, text: str):
        self.text = text

def get_sentence_embeddings(content: str, max_length=512) -> list[list[float]]:
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
            outputs = model(
                input_ids=input_ids,
                attention_mask=attention_mask,
                token_type_ids=token_type_ids,
            )
        else:
            outputs = model(
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
    matrix_vec = get_sentence_embeddings(doc.text)
    return jsonify({'vec': matrix_vec})


@app.route('/summarize', methods=['POST'])
def summarize():
    doc_data = request.get_json()
    if not doc_data or 'text' not in doc_data:
        return jsonify({'error': 'Invalid input'}), 400

    text = doc_data['text']
    max_sentence = doc_data['max_sentence']
    summary = summarizer(text, max_sentence=max_sentence)
    if not summary:
        return jsonify({'error': 'Failed to summarize'}), 500
    return jsonify({'summary': summary})

# Run the Flask app
if __name__ == '__main__':
    app.run(debug=True, port=50920)