let name=request.getQuery("name")
let message=request.getQuery("data")
event.put("messages", JSON.stringify({
    name:name,
    data:message
}));

// response.write(event.subscribe("messages").get())