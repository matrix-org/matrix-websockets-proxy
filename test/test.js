var socket;

$(document).ready(onload);

function parseQueryString() {
    var queryDict = {}
    location.search.substr(1).split("&").forEach(function(item) {
        queryDict[item.split("=")[0]] = item.split("=")[1]
    });
    return queryDict;
}

function onload() {
    var queryDict = parseQueryString();
    if(queryDict["token"]) {
        $("#token").val(queryDict["token"])
    }
}

function start() {
    if(socket) {
        socket.close();
        socket = null;
    }
    clear_log();

    var access_token = $("#token")[0].value;
    var since = $("#since")[0].value;
    var url = "ws://"+window.location.host+"/stream" +
        "?access_token="+encodeURIComponent(access_token);
    if (since != "") {
        url += "&since="+encodeURIComponent(since);
    }

    try {
    	socket = new WebSocket(url, "m.json");
    } catch (err) {
    	append_message(err.message, "error");
    	return;
    }

    socket.onopen = function(ev) {
        console.log("Connected to "+url);
    }
    socket.onclose = function(ev) {
    	if (ev.wasClean) {
           message("Socket closed");
        } else {
           message("Unclean close. Code: "+ev.code+" reason: "+ev.reason,
                   "error");
        }
    }
    socket.onerror = function(ev) {
        message("error", "error");
    }
    socket.onmessage = function(ev) {
        message(ev.data);
        obj = JSON.parse(ev.data);
        $("#since").val(obj.next_batch);
    }
}

function stop() {
    if(socket) {
        socket.close();
        socket = null;
    }
}

function clear_log() {
    $("#log").empty();
}

function message(message, cls) {
    var log_elem = $("#log");
    var elem = $("<div/>");
    if (cls) {
        elem = elem.addClass(cls);
    }
    elem = elem.text(message);
    elem.appendTo(log_elem);
    log_elem.scrollTop(log_elem[0].scrollHeight);
}

