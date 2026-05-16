const app = http.router();

app.get("/helpers", async function(request) {
    const currentCookie = http.cookie(request, "demo");
    const cookieText = currentCookie == null ? "(none)" : currentCookie;

    return http.html(`<!doctype html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>HTTP Helpers</title>
</head>
<body>
<h1>HTTP Helpers</h1>
<p>Current demo cookie: <strong>${cookieText}</strong></p>
<ul>
    <li><a href="/helpers/set-cookie">/helpers/set-cookie</a></li>
    <li><a href="/helpers/clear-cookie">/helpers/clear-cookie</a></li>
    <li><a href="/helpers/read-cookie">/helpers/read-cookie</a></li>
    <li><a href="/helpers/headers">/helpers/headers</a></li>
    <li><a href="/helpers/redirect">/helpers/redirect</a></li>
    <li><a href="/helpers/error">/helpers/error</a></li>
    <li><a href="/helpers/no-content">/helpers/no-content</a></li>
    <li><a href="/helpers/method">GET /helpers/method</a> returns 405 because only PUT is registered.</li>
</ul>
<form action="/helpers/echo" method="post">
    <input name="message" value="hello helpers">
    <button type="submit">POST /helpers/echo</button>
</form>
</body>
</html>`);
});

app.get("/helpers/set-cookie", async function() {
    return await http.setCookie(http.text("cookie set"), "demo", "hello", {
        path: "/",
        httpOnly: true,
    });
});

app.get("/helpers/clear-cookie", async function() {
    return await http.clearCookie(http.text("cookie cleared"), "demo", {
        path: "/",
        httpOnly: true,
    });
});

app.get("/helpers/read-cookie", async function(request) {
    return http.json(http.cookie(request));
});

app.post("/helpers/echo", async function(request) {
    return http.json({
        body: await http.read(request, "form"),
    });
});

app.get("/helpers/headers", async function() {
    return await http.withHeaders(http.text("extra headers"), {
        "x-demo": "helpers",
    });
});

app.get("/helpers/redirect", async function() {
    return http.redirect("/helpers");
});

app.get("/helpers/error", async function() {
    return http.error(418, "short and stout");
});

app.get("/helpers/no-content", async function() {
    return http.noContent();
});

app.put("/helpers/method", async function() {
    return http.text("PUT ok");
});

serve(app);
