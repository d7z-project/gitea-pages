response.write("hello world")
console.log("hello world")
function testError(){
    throw Error("Method not implemented")
}
response.setHeader("content-type", "application/json")
testError()