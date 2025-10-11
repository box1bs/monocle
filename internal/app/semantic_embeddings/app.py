import torch
import joblib
import json
import numpy as np
from flask import Flask, request, jsonify
from transformers import BertConfig, BertModel, BertTokenizer
from sentence_transformers import SentenceTransformer

app = Flask(__name__)

all_mini_lm_path = './all-MiniLM-L6-v2'

config = BertConfig.from_json_file('model/config.json')
bert = BertModel.from_pretrained('model', config=config)
tokenizer = BertTokenizer.from_pretrained('model/vocab.txt', do_lower_case=True)

try:
    model = SentenceTransformer(all_mini_lm_path)
    print(f"Модель успешно загружена с '{all_mini_lm_path}'")
except Exception as e:
    print(f"ОШИБКА: Не удалось загрузить модель. Проверьте путь '{all_mini_lm_path}'. Ошибка: {e}")

lr_model = joblib.load("./ranking_model/model_linear_regression.pkl")
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

def chunk_text(text: str, chunk_size: int, stride: int) -> list[str]:
    tokenizer = model.tokenizer
    
    tokens = tokenizer.encode(text)
    
    if len(tokens) <= chunk_size:
        return [text]
        
    chunks = []
    for i in range(0, len(tokens), chunk_size - stride):
        chunk_tokens = tokens[i : i + chunk_size]
        chunks.append(tokenizer.decode(chunk_tokens, skip_special_tokens=True))
        
    return chunks

def get_sentence_embeddings(content: str, max_length=512, stride=50) -> list[list[float]]:
    text_chunks = chunk_text(content, chunk_size=max_length, stride=stride)
    
    embeddings = model.encode(text_chunks, convert_to_numpy=True, show_progress_bar=False)
    
    return embeddings.tolist()

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