import torch
from flask import Flask, request, jsonify
from sentence_transformers import SentenceTransformer

app = Flask(__name__)

all_mini_lm_path = 'all-MiniLM-L6-v2'

try:
    model = SentenceTransformer(all_mini_lm_path)
    print(f"Модель успешно загружена с '{all_mini_lm_path}' на устройство ")
except Exception as e:
    print(f"ОШИБКА: Не удалось загрузить модель. Проверьте путь '{all_mini_lm_path}'. Ошибка: {e}")

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

@app.route('/vectorize', methods=['POST'])
def get_embeddings():
    doc_data = request.get_json()
    if not doc_data or 'text' not in doc_data:
        return jsonify({'error': 'Invalid input'}), 400

    doc = Document(text=doc_data['text'])
    matrix_vec = get_sentence_embeddings(doc.text)
    return jsonify({'vec': matrix_vec})


# Run the Flask app
if __name__ == '__main__':
    app.run(debug=True, port=50920)