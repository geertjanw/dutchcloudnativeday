package com.dcnd.frontend;

import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestClient;

import java.util.LinkedHashMap;
import java.util.Map;

@RestController
public class RegistrationController {

    private final RestClient backend;

    public RegistrationController(RestClient backendRestClient) {
        this.backend = backendRestClient;
    }

    @GetMapping(value = "/", produces = MediaType.TEXT_HTML_VALUE)
    public String form() {
        return FORM_HTML;
    }

    @GetMapping("/healthz")
    public ResponseEntity<String> healthz() {
        return ResponseEntity.ok("ok");
    }

    @PostMapping(value = "/register", consumes = MediaType.APPLICATION_FORM_URLENCODED_VALUE,
            produces = MediaType.TEXT_HTML_VALUE)
    public ResponseEntity<String> register(
            @RequestParam(required = false) String name,
            @RequestParam(required = false) String email,
            @RequestParam(required = false) String company,
            @RequestParam(required = false, defaultValue = "standard") String ticketType) {

        Map<String, Object> body = new LinkedHashMap<>();
        body.put("name", name);
        body.put("email", email);
        body.put("company", company);
        body.put("ticketType", ticketType);

        try {
            @SuppressWarnings("unchecked")
            Map<String, Object> result = backend.post()
                    .uri("/api/register")
                    .contentType(MediaType.APPLICATION_JSON)
                    .body(body)
                    .retrieve()
                    .body(Map.class);

            String registrationId = result == null ? "" : String.valueOf(result.get("registrationId"));
            return ResponseEntity.ok(successHtml(name, email, registrationId));
        } catch (org.springframework.web.client.RestClientResponseException e) {
            // Backend responded with a classified error (400/422/503). Mirror its
            // status so the client/outage distinction stays visible, and show the
            // backend's human-readable message.
            String json = e.getResponseBodyAsString();
            String message = extractField(json, "message");
            String errorType = extractField(json, "errorType");
            int status = e.getStatusCode().value();
            return ResponseEntity.status(status).contentType(MediaType.TEXT_HTML)
                    .body(errorHtml(message, errorType));
        } catch (Exception e) {
            // Backend unreachable — this is a gateway problem, so 502.
            return ResponseEntity.status(502).contentType(MediaType.TEXT_HTML)
                    .body(errorHtml("could not reach backend: " + e.getMessage(), null));
        }
    }

    /** Minimal, dependency-free extraction of a top-level string field. */
    private static String extractField(String jsonBody, String field) {
        if (jsonBody == null) {
            return "unknown error";
        }
        int i = jsonBody.indexOf("\"" + field + "\"");
        if (i < 0) {
            return jsonBody;
        }
        int colon = jsonBody.indexOf(':', i);
        int firstQuote = jsonBody.indexOf('"', colon + 1);
        int endQuote = jsonBody.indexOf('"', firstQuote + 1);
        if (firstQuote < 0 || endQuote < 0) {
            return jsonBody;
        }
        return jsonBody.substring(firstQuote + 1, endQuote);
    }

    private static String escape(String s) {
        if (s == null) {
            return "";
        }
        return s.replace("&", "&amp;")
                .replace("<", "&lt;")
                .replace(">", "&gt;")
                .replace("\"", "&quot;");
    }

    private static String successHtml(String name, String email, String registrationId) {
        return """
            <!doctype html><html lang="en"><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width, initial-scale=1">
            <title>Registered · Dutch Cloud Native Day</title>
            <style>%s</style></head>
            <body><div class="card">
              <div class="head" style="background:linear-gradient(135deg,#16a34a,#22c55e)">
                <h1>You're registered 🎉</h1><p>Dutch Cloud Native Day</p></div>
              <div style="padding:24px 28px 28px;font-size:14px;line-height:1.6">
                <p>Thanks, <strong>%s</strong>! A confirmation is on its way to
                   <strong>%s</strong>.</p>
                <p style="color:#5b6472">Registration id: <code>%s</code></p>
                <p><a href="/">← Register someone else</a></p>
              </div></div></body></html>
            """.formatted(SHARED_STYLE, escape(name), escape(email), escape(registrationId));
    }

    private static String errorHtml(String error, String errorType) {
        String typeLine = (errorType == null || errorType.isBlank())
                ? ""
                : "<p style=\"color:#5b6472\">Type: <code>" + escape(errorType) + "</code></p>";
        return """
            <!doctype html><html lang="en"><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width, initial-scale=1">
            <title>Registration failed · Dutch Cloud Native Day</title>
            <style>%s</style></head>
            <body><div class="card">
              <div class="head" style="background:linear-gradient(135deg,#dc2626,#ef4444)">
                <h1>Registration failed</h1><p>Dutch Cloud Native Day</p></div>
              <div style="padding:24px 28px 28px;font-size:14px;line-height:1.6">
                <p>Something went wrong processing your registration.</p>
                <p style="color:#5b6472">Error: <code>%s</code></p>
                %s
                <p>This is expected in the workshop — now go find this trace in Dash0 and
                   see which span failed.</p>
                <p><a href="/">← Try again</a></p>
              </div></div></body></html>
            """.formatted(SHARED_STYLE, escape(error), typeLine);
    }

    // Shared :root + body + card + head styles (identical to the registration page).
    private static final String SHARED_STYLE = """
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
        """;

    // Registration page, served verbatim per SPEC.
    private static final String FORM_HTML = """
        <!doctype html>
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
        </html>
        """;
}
