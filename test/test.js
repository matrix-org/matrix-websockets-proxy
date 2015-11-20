var socket = new WebSocket("ws://localhost:8800/ws", "m.json");

function send(ev) {
    var msg = {
        type: "message",
        text: document.getElementById("text").value,
        date: Date.now()
    };

    var json = JSON.stringify(msg)
    
    console.log("sending "+json)
    socket.send(json);
}

