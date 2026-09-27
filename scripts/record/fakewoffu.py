#!/usr/bin/env python3
"""A tiny fake Woffu for recording 'woffux setup' (scripts/record-setup.sh).

It answers only what setup asks — the account lookup, the sign-in and the
profile — for a made-up person at a made-up company. Nothing here is real.

    python3 scripts/record/fakewoffu.py PORT
"""
import http.server
import json
import sys
from urllib.parse import urlparse

PROFILE = {
    "UserId": 1001,
    "FullName": "ANA GARCÍA LÓPEZ",
    "Email": "ana@acme.com",
    "CompanyName": "Acme",
    "DepartmentFullName": "Operations",
    "JobTitleName": "Product manager",
    "OfficeName": "Acme Madrid",
    # A public square in Madrid (Plaza de Colón), not anyone's office.
    "OfficeLatitude": 40.4251,
    "OfficeLongitude": -3.6907,
}

ROUTES = {
    "/svc/accounts/authorization/use-new-login": {"useNewLogin": True, "companyId": 1},
    "/svc/accounts/companies/login-configuration-by-email": {
        "providerName": None, "openIdLogin": False, "domain": "acme.woffu.com", "woffuLogin": True, "autoLogin": False,
    },
    "/api/svc/accounts/authorization/users/token": {"token": "fake-token-for-the-recording"},
    "/api/users": PROFILE,
}


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        body = ROUTES.get(urlparse(self.path).path)
        self.reply(200 if body is not None else 404, body if body is not None else {"error": "not found"})

    def do_POST(self):
        if urlparse(self.path).path == "/svc/accounts/authorization/token":
            self.send_response(200)
            self.send_header("Set-Cookie", "woffu.session=fake; Path=/")
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        self.reply(404, {"error": "not found"})


http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
