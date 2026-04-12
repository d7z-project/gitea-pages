const app = http.router();

app.get("/api", async () => {
    return http.html(`
<!doctype html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Router Example</title>
</head>
<body>
<h1>Router Example</h1>
<ul>
    <li><a href="/api/hello">/api/hello</a></li>
    <li><a href="/api/users/42?tab=profile">/api/users/42?tab=profile</a></li>
    <li><a href="/api/missing">/api/missing</a></li>
</ul>
<form action="/api/echo" method="post">
    <input type="text" name="message" value="hello router">
    <button type="submit">POST /api/echo</button>
</form>
</body>
</html>
`);
});

app.get("/api/hello", async () => {
    return http.text("hello from router");
});

app.get("/api/users/:id", async (request, ctx) => {
    return http.json({
        id: ctx.params.id,
        tab: ctx.query.get("tab"),
        method: request.method,
        repo: page.meta.repo,
    });
});

app.post("/api/echo", async (request) => {
    const body = await http.read(request, "form");
    return http.json({
        ok: true,
        body,
    });
});

app.all("/api/*", async () => {
    return http.notFound("router example: route not found");
});

serve(app);
