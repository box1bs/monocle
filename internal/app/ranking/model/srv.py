import pickle
import numpy as np # type: ignore
from fastapi import FastAPI, HTTPException # type: ignore
from pydantic import BaseModel # type: ignore
from tensorflow.keras.preprocessing.sequence import pad_sequences # type: ignore
from tensorflow.keras.models import load_model # type: ignore
import uvicorn # type: ignore


with open('vocab_m2.pickle', 'rb') as f:
    vocab = pickle.load(f)
vocab_size = len(vocab) + 1


max_query_len = 10
max_doc_len = 256


model = load_model('ranking_model_m2.h5')


def tokens_to_sequence(tokens: list[str]):
    return [vocab.get(token, 0) for token in tokens]


class PredictRequest(BaseModel):
    query: list[str]
    documents: list[list[str]]


app = FastAPI(title="Fast Ranking API")


@app.post("/predict")
async def predict(request: PredictRequest):
    query_tokens = request.query
    doc_tokens_list = request.documents

    if not query_tokens or not doc_tokens_list:
        raise HTTPException(status_code=400, detail="empty query or documents")


    query_seq = tokens_to_sequence(query_tokens)
    query_seq = pad_sequences([query_seq], maxlen=max_query_len)


    doc_sequences = [tokens_to_sequence(doc_tokens) for doc_tokens in doc_tokens_list]
    doc_sequences = pad_sequences(doc_sequences, maxlen=max_doc_len)


    query_input = np.repeat(query_seq, len(doc_tokens_list), axis=0)


    predictions = model.predict([query_input, doc_sequences])
    predictions = predictions.flatten().tolist()


    print(f"Query tokens: {query_tokens}")
    print(f"Query sequence: {query_seq}")


    return [{"document": " ".join(doc_tokens), "score": score}
            for doc_tokens, score in zip(doc_tokens_list, predictions)]

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8000, workers=1)