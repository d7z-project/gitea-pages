(async ()=>{
    let ws = websocket();
    while (true) {
        let data = await ws.readText();
        switch (data) {
            case "exit":
                return
            case "panic":
                throw Error("错误");
            case "date":
                await ws.writeText(new Date().toJSON())
                break
            default:
                await ws.writeText("收到信息:" + data)
                break;
        }
    }
})()