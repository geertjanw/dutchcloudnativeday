# OpenTelemetry must be set up BEFORE the Flask app is created.
import otel_setup  # noqa: F401  (import for side effects: provider + instrumentation)
from otel_setup import instrument_app

import os
from html import escape

import requests
from flask import Flask, request

app = Flask(__name__)
instrument_app(app)

BACKEND_URL = os.environ.get("BACKEND_URL", "http://localhost:3101")

# Shared styles reused by the registration page and the result pages.
STYLE = """
  :root { --ink:#0b1020; --accent:#ff6a13; --accent2:#1f6feb; --line:#e5e7eb; --muted:#5b6472; }
  * { box-sizing:border-box; }
  body { margin:0; font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
         background:linear-gradient(135deg,#0b1020,#13224a); color:var(--ink); min-height:100vh;
         display:flex; align-items:center; justify-content:center; padding:24px; }
  .card { background:#fff; border-radius:16px; box-shadow:0 20px 60px rgba(0,0,0,.35); width:100%; max-width:460px; overflow:hidden; }
  .head { background:linear-gradient(135deg,var(--accent),#ff8f4d); color:#fff; padding:28px 28px 22px; }
  .head h1 { margin:0; font-size:22px; letter-spacing:.2px; }
  .head p { margin:6px 0 0; opacity:.95; font-size:13px; }
  form { padding:24px 28px 28px; }
  label { display:block; font-size:13px; font-weight:600; margin:14px 0 6px; }
  input, select { width:100%; padding:11px 12px; border:1px solid var(--line); border-radius:9px; font-size:14px; }
  input:focus, select:focus { outline:2px solid var(--accent2); border-color:transparent; }
  button { margin-top:22px; width:100%; padding:13px; border:0; border-radius:9px; background:var(--accent2);
           color:#fff; font-size:15px; font-weight:600; cursor:pointer; }
  button:hover { background:#1a5fd0; }
  .hint { margin-top:16px; font-size:12px; color:var(--muted); line-height:1.5; }
  .hint code { background:#f1f3f5; padding:1px 5px; border-radius:4px; }
"""

REGISTRATION_PAGE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Register · Dutch Cloud Native Day</title>
<style>
__STYLE__
</style>
</head>
<body>
  <div class="card">
    <div class="head">
      <h1>Dutch Cloud Native Day</h1>
      <p>Register to attend · From One Request to One Trace</p>
    </div>
    <form method="post" action="/register">
      <label for="name">Full name</label>
      <input id="name" name="name" required placeholder="Ada Lovelace">
      <label for="email">Email</label>
      <input id="email" name="email" type="email" required placeholder="ada@example.com">
      <label for="company">Company</label>
      <input id="company" name="company" placeholder="Analytical Engines">
      <label for="ticketType">Ticket type</label>
      <select id="ticketType" name="ticketType">
        <option value="standard">Standard</option>
        <option value="workshop">Workshop</option>
        <option value="vip">VIP</option>
      </select>
      <button type="submit">Register</button>
      <p class="hint">Tip for the workshop: register with an email containing
        <code>+fail</code> (e.g. <code>ada+fail@example.com</code>) to force an
        error trace in Dash0. Otherwise ~15% of registrations fail at random.</p>
    </form>
  </div>
</body>
</html>
""".replace("__STYLE__", STYLE)

SUCCESS_PAGE = """<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Registered · Dutch Cloud Native Day</title>
<style>__STYLE__</style></head>
<body><div class="card">
  <div class="head" style="background:linear-gradient(135deg,#16a34a,#22c55e)">
    <h1>You're registered 🎉</h1><p>Dutch Cloud Native Day</p></div>
  <div style="padding:24px 28px 28px;font-size:14px;line-height:1.6">
    <p>Thanks, <strong>__NAME__</strong>! A confirmation is on its way to
       <strong>__EMAIL__</strong>.</p>
    <p style="color:#5b6472">Registration id: <code>__REGID__</code></p>
    <p><a href="/">← Register someone else</a></p>
  </div></div></body></html>""".replace("__STYLE__", STYLE)

ERROR_PAGE = """<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Registration failed · Dutch Cloud Native Day</title>
<style>__STYLE__</style></head>
<body><div class="card">
  <div class="head" style="background:linear-gradient(135deg,#dc2626,#ef4444)">
    <h1>Registration failed</h1><p>Dutch Cloud Native Day</p></div>
  <div style="padding:24px 28px 28px;font-size:14px;line-height:1.6">
    <p>Something went wrong processing your registration.</p>
    <p style="color:#5b6472">Error: <code>__ERROR__</code></p>
    <p>This is expected in the workshop — now go find this trace in Dash0 and
       see which span failed.</p>
    <p><a href="/">← Try again</a></p>
  </div></div></body></html>""".replace("__STYLE__", STYLE)


@app.get("/")
def index():
    return REGISTRATION_PAGE


@app.post("/register")
def register():
    name = request.form.get("name", "")
    email = request.form.get("email", "")
    company = request.form.get("company", "")
    ticket_type = request.form.get("ticketType", "standard")

    payload = {
        "name": name,
        "email": email,
        "company": company,
        "ticketType": ticket_type,
    }

    try:
        # RequestsInstrumentor injects traceparent so this joins the trace.
        resp = requests.post(
            BACKEND_URL + "/api/register", json=payload, timeout=10
        )
    except Exception as exc:  # backend actually unreachable → 502
        return ERROR_PAGE.replace("__ERROR__", escape(str(exc))), 502

    try:
        data = resp.json()
    except ValueError:
        data = {}

    if resp.status_code == 200 and data.get("status") == "confirmed":
        page = (
            SUCCESS_PAGE.replace("__NAME__", escape(name))
            .replace("__EMAIL__", escape(email))
            .replace("__REGID__", escape(str(data.get("registrationId", ""))))
        )
        return page, 200

    # Mirror the backend's status class (400/422/503) and show its message.
    message = data.get("message") or "Unknown error"
    error_type = data.get("errorType")
    shown = message if not error_type else "{} ({})".format(message, error_type)
    return ERROR_PAGE.replace("__ERROR__", escape(str(shown))), resp.status_code


@app.get("/healthz")
def healthz():
    return "ok", 200


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "3100"))
    # use_reloader off so the tracer provider is not initialised twice.
    app.run(host="0.0.0.0", port=port, debug=False, use_reloader=False)
