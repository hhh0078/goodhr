from __future__ import annotations

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


HOST = "127.0.0.1"
PORT = int(os.getenv("GOODHR_AGENT_PORT", "9001"))


class Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        if self.path == "/health":
            self._json(
                {
                    "ok": True,
                    "name": "GoodHR 5 Local Agent",
                    "version": "0.1.0",
                    "machine_id": "",
                }
            )
            return

        self.send_error(404)

    def do_OPTIONS(self) -> None:
        self.send_response(204)
        self._cors_headers()
        self.end_headers()

    def log_message(self, fmt: str, *args: object) -> None:
        return

    def _json(self, payload: dict) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(200)
        self._cors_headers()
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _cors_headers(self) -> None:
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-GoodHR-Local-Token")
        self.send_header("Access-Control-Allow-Private-Network", "true")


def main() -> None:
    server = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"GoodHR 5 Local Agent listening on http://{HOST}:{PORT}")
    server.serve_forever()


if __name__ == "__main__":
    main()
