response.setHeader("content-type", "application/json");
response.write(JSON.stringify({
    'method': request.method,
}))
