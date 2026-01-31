FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py .

ENV PORT=8080
ENV WORKER_NAME=python-worker-1
ENV WORKER_COLOR=#10B981
ENV MAX_CONCURRENT_REQUESTS=10
ENV RESPONSE_DELAY_MS=100
ENV FAILURE_RATE=0.0
ENV QUEUE_SIZE=50

EXPOSE 8080

CMD ["python", "main.py"]
