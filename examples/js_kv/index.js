const db = kv.repo("self");
if(request.path === "put"){
    data = db.get('key')
    if(data == null){
        db.set('key','0')
    }else {
        db.set('key',(parseInt(data)+1).toString())
    }
}
response.write("当前存储的数值为 " + db.get('key'))

const test = kv.repo("test");
for (let i = 0; i < 500; i++) {
    test.set("key" + i,"value" + i);
}
const list = test.list();
console.log(list.keys.length)
console.log(list.cursor)
console.log(list.hasNext)
