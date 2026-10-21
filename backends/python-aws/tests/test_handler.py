"""The Lambda adapter serves the same ASGI app the local server uses."""

import json

from dashboard_api.handler import handler
from dashboard_api.main import APP_RUNTIME


def test_default_runtime_is_lambda():
    assert APP_RUNTIME == "lambda"


def test_function_url_healthz():
    event = {
        "version": "2.0",
        "routeKey": "$default",
        "rawPath": "/healthz",
        "rawQueryString": "",
        "headers": {"host": "blue.lambda-url.us-west-2.on.aws"},
        "requestContext": {
            "accountId": "000000000000",
            "apiId": "python",
            "domainName": "blue.lambda-url.us-west-2.on.aws",
            "domainPrefix": "blue",
            "http": {
                "method": "GET",
                "path": "/healthz",
                "protocol": "HTTP/1.1",
                "sourceIp": "127.0.0.1",
                "userAgent": "test",
            },
            "requestId": "id",
            "routeKey": "$default",
            "stage": "$default",
            "time": "21/Oct/2026:00:00:00 +0000",
            "timeEpoch": 1761000000000,
        },
        "isBase64Encoded": False,
    }
    result = handler(event, None)
    assert result["statusCode"] == 200
    body = json.loads(result["body"])
    assert body["runtime"] == "lambda"
    assert body["lang"] == "python"
    assert body["backend"] == "aws-python"
