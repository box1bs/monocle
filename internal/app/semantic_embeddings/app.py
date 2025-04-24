import torch
from flask import Flask, request, jsonify
from transformers import BertConfig, BertModel, BertTokenizer

# Initialize Flask app
app = Flask(__name__)

config = BertConfig.from_json_file('model/config.json')
model = BertModel.from_pretrained('model', config=config)
tokenizer = BertTokenizer.from_pretrained('model/vocab.txt', do_lower_case=True)

# Define a simple Document class to hold the list of words
class Document:
    text: str
    def __init__(self, text: str):
        self.text = text

# Function to generate sentence embeddings using BERT
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

# Flask endpoint to vectorize text
@app.route('/vectorize', methods=['POST'])
def get_embeddings():
    # Get JSON data from the request
    doc_data = request.get_json()
    if not doc_data or 'text' not in doc_data:
        return jsonify({'error': 'Invalid input'}), 400

    # Create a Document instance and generate embeddings
    doc = Document(text=doc_data['text'])
    matrix_vec = get_sentence_embeddings(doc.text)
    return jsonify({'vec': matrix_vec})

# Run the Flask app
if __name__ == '__main__':
    app.run(debug=True, port=50920)