"""Lambda Function URL adapter for the FastAPI app.

Local kind and `make demo` keep serving through uvicorn
(`python -m uvicorn dashboard_api.main:app`). The cloud image
(`Dockerfile.lambda`) sets this module's `handler` as the Lambda entrypoint.
"""

from mangum import Mangum

from dashboard_api.main import app

handler = Mangum(app, lifespan="off")
