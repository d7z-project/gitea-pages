resp.setHeader("content-type", "application/json");
resp.write(JSON.stringify({
    'method': req.method,
}))
