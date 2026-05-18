from __future__ import annotations

import json
import os
from collections.abc import Iterable
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


HOST = "127.0.0.1"
DEFAULT_PORTS = range(9001, 9010)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        if self.path == "/health":
            self._json(
                {
                    "ok": True,
                    "name": "GoodHR 5 Local Agent",
                    "version": "0.1.0",
                    "port": self.server.server_address[1],
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
    server = create_server()
    host, port = server.server_address
    print(f"GoodHR 5 Local Agent listening on http://{host}:{port}")
    server.serve_forever()


def create_server() -> ThreadingHTTPServer:
    errors: list[str] = []
    for port in candidate_ports():
        try:
            return ThreadingHTTPServer((HOST, port), Handler)
        except OSError as exc:
            errors.append(f"{port}: {exc}")

    detail = "; ".join(errors)
    raise RuntimeError(f"No available GoodHR Local Agent port in 9001-9009. {detail}")


def candidate_ports() -> Iterable[int]:
    configured = os.getenv("GOODHR_AGENT_PORT")
    yielded: set[int] = set()

    if configured:
        try:
            port = int(configured)
        except ValueError as exc:
            raise RuntimeError("GOODHR_AGENT_PORT must be a number") from exc

        yielded.add(port)
        yield port

    for port in DEFAULT_PORTS:
        if port in yielded:
            continue
        yield port


if __name__ == "__main__":
    main()
