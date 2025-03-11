import pickle
import numpy as np # type: ignore
from fastapi import FastAPI, HTTPException # type: ignore
from pydantic import BaseModel # type: ignore
import nltk # type: ignore
from nltk.stem import PorterStemmer # type: ignore
from tensorflow.keras.preprocessing.sequence import pad_sequences # type: ignore
from tensorflow.keras.models import load_model # type: ignore
import uvicorn # type: ignore

nltk.download('punkt_tab', quiet=True)
stemmer = PorterStemmer()

with open('vocab.pickle', 'rb') as f:
    vocab = pickle.load(f)
vocab_size = len(vocab) + 1

max_query_len = 10
max_doc_len = 256
model = load_model('ranking_model_m1.h5')


def tokenize(text: str):
    return [stemmer.stem(word.lower()) for word in nltk.word_tokenize(text)]


def text_to_sequence(text: str):
    tokens = tokenize(text)
    return [vocab.get(token, 0) for token in tokens]


class PredictRequest(BaseModel):
    query: str
    documents: list[str]


app = FastAPI(title="Fast Ranking API")


@app.post("/predict")
async def predict(request: PredictRequest):
    query_text = request.query
    documents = request.documents

    if not query_text or not documents:
        raise HTTPException(status_code=400, detail="empty query or documents")

    query_seq = text_to_sequence(query_text)
    query_seq = pad_sequences([query_seq], maxlen=max_query_len)

    doc_sequences = [text_to_sequence(doc) for doc in documents]
    doc_sequences = pad_sequences(doc_sequences, maxlen=max_doc_len)

    query_input = np.repeat(query_seq, len(documents), axis=0)

    predictions = model.predict([query_input, doc_sequences])
    predictions = predictions.flatten().tolist()

    print(f"Query: {query_text}")
    print(f"Query sequence: {query_seq}")

    return [{"document": doc, "score": score} for doc, score in zip(documents, predictions)]


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8000, workers=1)