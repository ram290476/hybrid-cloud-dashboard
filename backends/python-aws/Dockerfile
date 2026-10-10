FROM python:3.12-slim
WORKDIR /app
COPY backends/python-aws /app
COPY contract/fixtures /fixtures
RUN pip install --no-cache-dir fastapi uvicorn boto3
ENV FIXTURES_DIR=/fixtures PORT=8080 PYTHONPATH=/app/src
EXPOSE 8080
USER nobody
CMD ["python", "-m", "uvicorn", "dashboard_api.main:app", "--host", "0.0.0.0", "--port", "8080"]
