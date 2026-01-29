(async ()=>{
    let ws = websocket();
    while (true) {
        let data = await ws.readText();
        await ws.writeText("ECHO: " + data)
    }
})()