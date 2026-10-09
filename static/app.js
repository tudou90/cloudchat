let socket = null;
let currentRoom = null;
let userName = "User_" + Math.floor(Math.random() * 1000);

const landing = document.getElementById('landing');
const chatRoom = document.getElementById('chat-room');
const messagesDiv = document.getElementById('messages');
const messageInput = document.getElementById('message-input');
const displayRoomId = document.getElementById('display-room-id');

// Create Room
document.getElementById('create-room').onclick = async () => {
    const res = await fetch('/api/rooms', { method: 'POST' });
    const data = await res.json();
    joinRoom(data.id);
};

// Join Room
document.getElementById('join-room').onclick = () => {
    const id = document.getElementById('join-id').value.trim();
    if (id) joinRoom(id);
};

function joinRoom(id) {
    currentRoom = id;
    displayRoomId.innerText = `Room: ${id.substring(0, 8)}...`;
    displayRoomId.title = id; // Full ID on hover
    
    // Switch screens
    landing.classList.remove('active');
    setTimeout(() => {
        chatRoom.classList.add('active');
        connectWS(id);
    }, 500);
}

function connectWS(id) {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(`${protocol}//${window.location.host}/ws/${id}`);

    socket.onmessage = (event) => {
        const messages = event.data.split('\n');
        messages.forEach(msgStr => {
            if (!msgStr) return;
            try {
                const msg = JSON.parse(msgStr);
                appendMessage(msg);
            } catch (e) {
                console.error("Error parsing message:", e);
            }
        });
    };

    socket.onclose = () => {
        console.log("Disconnected from WebSocket");
    };
    
    socket.onerror = (err) => {
        console.error("WebSocket error:", err);
    };
}

function appendMessage(msg) {
    const isSelf = msg.sender === userName;
    const div = document.createElement('div');
    div.className = `message ${isSelf ? 'self' : ''}`;
    
    const time = new Date(msg.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    
    div.innerHTML = `
        <div class="msg-info">${isSelf ? 'You' : msg.sender} • ${time}</div>
        <div class="msg-content">${escapeHTML(msg.content)}</div>
    `;
    
    messagesDiv.appendChild(div);
    messagesDiv.scrollTop = messagesDiv.scrollHeight;
}

function sendMessage() {
    const content = messageInput.value.trim();
    if (content && socket && socket.readyState === WebSocket.OPEN) {
        const msg = {
            type: 'chat',
            sender: userName,
            content: content
        };
        socket.send(JSON.stringify(msg));
        messageInput.value = '';
    }
}

function escapeHTML(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

document.getElementById('send-btn').onclick = sendMessage;
messageInput.onkeypress = (e) => {
    if (e.key === 'Enter') sendMessage();
};

document.getElementById('leave-room').onclick = () => {
    if (socket) socket.close();
    chatRoom.classList.remove('active');
    setTimeout(() => {
        landing.classList.add('active');
        messagesDiv.innerHTML = '';
        currentRoom = null;
    }, 500);
};
