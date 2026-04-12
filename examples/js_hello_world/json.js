serve(async function(request) {
    return Response.json({
        method: request.method,
    });
});
