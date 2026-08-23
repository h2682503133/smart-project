FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY shared/ /app/shared/
COPY agent-service/ /app/agent-service/
COPY context-service/ /app/context-service/
COPY tool-service/ /app/tool-service/
COPY ingest-service/ /app/ingest-service/
COPY config.yaml /app/config.yaml

ENV PYTHONPATH=/app
