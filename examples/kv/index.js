var db = kv.repo("self");
if(request.path == "put"){
    data = db.get('key')
    if(data == undefined){
        db.set('key','0')
    }else {
        db.set('key',(parseInt(data)+1).toString())
    }
}
response.write("当前存储的数值为 " + db.get('key'))
