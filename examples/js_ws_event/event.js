let name=request.getQuery("name")
if (name===""){
    throw Error(`Missing name "${name}"`)
}
let ws = websocket();
event.subscribe("messages").on(function (msg){
    ws.writeText(msg)
})
let shouldExit = false;
while (!shouldExit) {
    let data = ws.readText();
    switch (data) {
        case "exit":
            shouldExit = true;
            break;
        default:
            event.put("messages",JSON.stringify({
                name:name,
                data:data
            }));
            break;
    }
}