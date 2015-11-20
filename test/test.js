var socket;

function start() {
    if(socket) {
        socket.close();
        socket = null;
    }
    clear_log();
    
    var access_token = document.getElementById("token").value;
    var url = "ws://"+window.location.host+"/stream" +
        "?access_token="+encodeURIComponent(access_token);

    socket = new WebSocket(url, "m.json");
    socket.onopen = function(ev) {
        console.log("Connected to "+url);
    }
    socket.onmessage = append_message;
}

function clear_log() {
    $("#log").empty();
}

function append_message(ev) {
    console.log("Got message "+ev.data)
    new_elem = $("<div/>").text(ev.data)
    new_elem.appendTo($("#log"))
}

function send(ev) {
    var msg = {
        type: "message",
        text: $("#text").value,
        date: Date.now()
    };

    var json = JSON.stringify(msg)
    
    console.log("sending "+json)
    socket.send(json);
}

