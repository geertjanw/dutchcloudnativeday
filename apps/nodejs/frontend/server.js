'use strict';

const express = require('express');

const app = express();
app.use(express.urlencoded({ extended: true }));

const PORT = process.env.PORT || 3000;
const BACKEND_URL = process.env.BACKEND_URL || 'http://localhost:3001';

// Registration page, served verbatim from the SPEC.
const PAGE = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Register · Dutch Cloud Native Day</title>
<style>
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
</html>`;

// Minimal HTML escaping for values substituted into the result pages.
function esc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

const SHARED_STYLE = `<style>
  :root { --ink:#0b1020; --accent:#ff6a13; --accent2:#1f6feb; --line:#e5e7eb; --muted:#5b6472; }
  * { box-sizing:border-box; }
  body { margin:0; font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
         background:linear-gradient(135deg,#0b1020,#13224a); color:var(--ink); min-height:100vh;
         display:flex; align-items:center; justify-content:center; padding:24px; }
  .card { background:#fff; border-radius:16px; box-shadow:0 20px 60px rgba(0,0,0,.35); width:100%; max-width:460px; overflow:hidden; }
  .head { background:linear-gradient(135deg,var(--accent),#ff8f4d); color:#fff; padding:28px 28px 22px; }
  .head h1 { margin:0; font-size:22px; letter-spacing:.2px; }
  .head p { margin:6px 0 0; opacity:.95; font-size:13px; }
</style>`;

function successPage({ name, email, registrationId }) {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Registered · Dutch Cloud Native Day</title>
${SHARED_STYLE}</head>
<body><div class="card">
  <div class="head" style="background:linear-gradient(135deg,#16a34a,#22c55e)">
    <h1>You're registered 🎉</h1><p>Dutch Cloud Native Day</p></div>
  <div style="padding:24px 28px 28px;font-size:14px;line-height:1.6">
    <p>Thanks, <strong>${esc(name)}</strong>! A confirmation is on its way to
       <strong>${esc(email)}</strong>.</p>
    <p style="color:#5b6472">Registration id: <code>${esc(registrationId)}</code></p>
    <p><a href="/">← Register someone else</a></p>
  </div></div></body></html>`;
}

function errorPage({ error, errorType }) {
  const typeLine = errorType
    ? `\n    <p style="color:#5b6472">Type: <code>${esc(errorType)}</code></p>`
    : '';
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Registration failed · Dutch Cloud Native Day</title>
${SHARED_STYLE}</head>
<body><div class="card">
  <div class="head" style="background:linear-gradient(135deg,#dc2626,#ef4444)">
    <h1>Registration failed</h1><p>Dutch Cloud Native Day</p></div>
  <div style="padding:24px 28px 28px;font-size:14px;line-height:1.6">
    <p>Something went wrong processing your registration.</p>
    <p style="color:#5b6472">Error: <code>${esc(error)}</code></p>${typeLine}
    <p>This is expected in the workshop — now go find this trace in Dash0 and
       see which span failed.</p>
    <p><a href="/">← Try again</a></p>
  </div></div></body></html>`;
}

app.get('/healthz', (_req, res) => {
  res.status(200).send('ok');
});

app.get('/', (_req, res) => {
  res.type('html').send(PAGE);
});

app.post('/register', async (req, res) => {
  const { name, email, company, ticketType } = req.body || {};

  try {
    // The undici auto-instrumentation injects `traceparent` into this fetch,
    // so the backend span joins the same distributed trace.
    const resp = await fetch(BACKEND_URL + '/api/register', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ name, email, company, ticketType }),
    });

    const data = await resp.json().catch(() => ({}));

    if (resp.ok && data.status === 'confirmed') {
      res.status(200).type('html').send(
        successPage({ name, email, registrationId: data.registrationId })
      );
    } else {
      // Mirror the backend's status class (400/422/503) so the client/outage
      // distinction stays visible; do not flatten it to a single code.
      res.status(resp.status).type('html').send(
        errorPage({
          error: data.message || `backend returned ${resp.status}`,
          errorType: data.errorType,
        })
      );
    }
  } catch (err) {
    // 502 only when the backend is actually unreachable.
    res.status(502).type('html').send(
      errorPage({ error: `backend unreachable: ${err.message}` })
    );
  }
});

app.listen(PORT, () => {
  console.log(`[registration-web] listening on :${PORT} → backend ${BACKEND_URL}`);
});
