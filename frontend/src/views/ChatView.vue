<template>
  <!-- Image preview -->
  <div
    v-if="preview"
    class="fixed inset-0 z-50 flex flex-col items-center justify-center gap-4 p-4 sm:p-6 bg-slate-950/90 backdrop-blur-sm animate-fade-in"
    @click.self="preview = null"
  >
    <img
      :src="preview.url"
      :alt="preview.name"
      class="max-w-full max-h-[80vh] rounded-xl shadow-2xl object-contain"
      @click="preview = null"
    >
    <div class="flex flex-wrap items-center justify-center gap-3 text-sm">
      <span class="text-slate-400 truncate max-w-[80vw] sm:max-w-[50vw]">{{ preview.name }} · {{ formatSize(preview.size) }}</span>
      <a
        :href="preview.url"
        :download="preview.name"
        class="px-3 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-white transition"
      >
        Download
      </a>
      <button
        @click="preview = null"
        class="px-3 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-white transition"
      >
        Close
      </button>
    </div>
  </div>

  <!-- Landing Screen -->
  <div v-if="!joined" class="min-h-screen flex items-center justify-center px-4 animate-slide-up">
    <div class="w-full max-w-md text-center">

      <!-- Logo (links back to the home page) -->
      <a href="/" class="inline-block hover:opacity-90 transition" title="CloudChat home">
        <div class="text-7xl mb-4 animate-float select-none">☁️</div>
        <h1 class="text-5xl font-semibold gradient-text mb-2 tracking-tight">CloudChat</h1>
      </a>
      <p class="text-slate-400 mb-10 text-base">Ephemeral. Secure. Gone when everyone leaves.</p>

      <!-- Card -->
      <div class="glass rounded-3xl p-6 sm:p-8 space-y-5">
        <!-- Invite banner -->
        <div v-if="invited" class="text-sm text-sky-300 bg-sky-400/10 border border-sky-400/20 rounded-xl px-4 py-3">
          You've been invited to a session. Enter a nickname and join.
        </div>

        <!-- Name input -->
        <input
          ref="nameInput"
          v-model="profile.name"
          @keyup.enter="invited ? joinRoom() : createRoom()"
          type="text"
          maxlength="20"
          placeholder="Your nickname..."
          class="w-full bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-white placeholder-slate-500 outline-none focus:border-sky-400/50 focus:ring-1 focus:ring-sky-400/30 transition"
        >

        <!-- Create -->
        <button
          v-if="!invited"
          @click="createRoom"
          class="w-full gradient-bg text-white font-semibold py-3 rounded-xl hover:opacity-90 active:scale-95 transition-all shadow-lg shadow-sky-500/20"
        >
          Start New Session
        </button>

        <div v-if="!invited" class="flex items-center gap-3 text-slate-600 text-sm">
          <div class="flex-1 h-px bg-white/5"></div>
          or join existing
          <div class="flex-1 h-px bg-white/5"></div>
        </div>

        <!-- Join -->
        <div class="flex gap-2">
          <input
            v-model="joinId"
            @keyup.enter="joinRoom"
            type="text"
            placeholder="Session ID or invite link"
            class="flex-1 min-w-0 bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-white placeholder-slate-500 outline-none focus:border-sky-400/50 transition"
          >
          <button
            @click="joinRoom"
            class="px-5 py-3 rounded-xl text-white font-semibold active:scale-95 transition-all"
            :class="invited
              ? 'gradient-bg hover:opacity-90 shadow-lg shadow-sky-500/20'
              : 'bg-white/5 border border-white/10 hover:bg-white/10'"
          >
            Join
          </button>
        </div>
      </div>

      <p class="mt-4 text-xs text-slate-600">
        By starting or joining a session you agree to our
        <a href="/terms" class="underline hover:text-slate-400">Terms</a> and
        <a href="/privacy" class="underline hover:text-slate-400">Privacy Policy</a>.
      </p>
      <a v-if="invited" :href="baseUrl" class="block mt-6 text-sm text-slate-500 hover:text-slate-300 transition">
        Start your own session instead
      </a>
      <div class="mt-6 flex flex-wrap justify-center gap-x-5 gap-y-2">
        <a href="/" class="text-sm text-slate-500 hover:text-slate-300 transition">← Home</a>
        <a href="secret" class="text-sm text-slate-500 hover:text-slate-300 transition">🔥 Send a self-destructing secret →</a>
      </div>
    </div>
  </div>

  <!-- Chat Room -->
  <div v-else class="h-[100dvh] sm:h-auto sm:min-h-screen flex items-center justify-center sm:p-4 animate-fade-in">
    <div
      class="glass rounded-3xl max-sm:rounded-none max-sm:border-0 w-full max-w-3xl h-full sm:h-[88vh] flex flex-col overflow-hidden relative"
      @dragenter.prevent="dragDepth++"
      @dragover.prevent
      @dragleave="dragDepth--"
      @drop.prevent="onDrop"
    >
      <!-- Drop overlay -->
      <div
        v-if="dragDepth > 0"
        class="absolute inset-0 z-10 flex items-center justify-center bg-slate-900/80 border-2 border-dashed border-sky-400/60 rounded-3xl text-sky-300 pointer-events-none"
      >
        Drop files to share (max {{ MAX_FILE_MB }} MB)
      </div>

      <!-- Header -->
      <div class="flex items-center justify-between gap-2 px-4 sm:px-6 py-3 sm:py-4 border-b border-white/10">
        <div class="flex items-center gap-2 sm:gap-3 min-w-0">
          <div
            class="w-2.5 h-2.5 rounded-full shrink-0"
            :class="connected ? 'bg-emerald-400 animate-pulse-glow' : reconnecting ? 'bg-amber-400 animate-pulse' : 'bg-red-400'"
          ></div>
          <!-- On phones the green dot alone says "live"; the text only shows when disconnected. -->
          <h2 class="font-semibold text-white whitespace-nowrap" :class="{ 'hidden sm:block': connected }">
            {{ connected ? 'Live Session' : reconnecting ? 'Reconnecting…' : 'Disconnected' }}
          </h2>
          <div v-if="connected && members.length" class="relative">
            <button
              @click="showMembers = !showMembers"
              class="text-xs px-2.5 py-1 rounded-lg text-slate-300 bg-white/5 hover:bg-white/10 transition whitespace-nowrap"
              title="Who's here"
            >
              👥 {{ members.length }} online
            </button>
            <div v-if="showMembers" class="fixed inset-0 z-20" @click="showMembers = false"></div>
            <div
              v-if="showMembers"
              class="absolute left-0 top-full mt-2 z-30 min-w-44 max-h-64 overflow-y-auto rounded-xl border border-white/10 bg-slate-900 p-2 shadow-xl"
            >
              <div
                v-for="m in members"
                :key="m.id"
                class="flex items-center gap-2 px-2 py-1.5 text-sm text-slate-200"
              >
                <span class="w-1.5 h-1.5 rounded-full bg-emerald-400 shrink-0"></span>
                <span class="truncate">{{ m.name }}</span>
                <span v-if="m.id === myId" class="text-xs text-slate-500 shrink-0">(you)</span>
              </div>
            </div>
          </div>
          <button
            @click="copyInvite"
            class="text-xs px-2.5 py-1 rounded-lg transition whitespace-nowrap"
            :class="copied ? 'bg-emerald-400/15 text-emerald-300' : 'text-slate-300 bg-white/5 hover:bg-white/10'"
            :title="inviteLink"
          >
            <template v-if="copied">✓ <span class="hidden sm:inline">Link </span>copied</template>
            <template v-else>🔗 <span class="hidden sm:inline">Copy invite link</span><span class="sm:hidden">Invite</span></template>
          </button>
        </div>
        <button
          @click="leaveRoom"
          class="shrink-0 text-sm px-4 py-1.5 rounded-xl border border-red-500/30 text-red-400 hover:bg-red-500/10 transition"
        >
          Exit
        </button>
      </div>

      <!-- Messages -->
      <div ref="messageBox" class="flex-1 overflow-y-auto px-3 sm:px-6 py-4 sm:py-5 space-y-4">
        <div
          v-for="(msg, i) in messages"
          :key="msg.id || i"
          class="flex animate-msg-in"
          :class="isSelf(msg) ? 'justify-end' : 'justify-start'"
        >
          <div
            class="max-w-[85%] sm:max-w-[72%] px-4 py-3 rounded-2xl"
            :class="isSelf(msg)
              ? 'gradient-bg text-white rounded-br-sm'
              : 'glass text-slate-100 rounded-bl-sm'"
          >
            <div class="text-xs mb-1 opacity-70">
              {{ isSelf(msg) ? 'You' : msg.sender }}
              &nbsp;·&nbsp;
              {{ formatTime(msg.time) }}
            </div>
            <template v-if="msg.type === 'file' && msg.file">
              <button
                v-if="isInlineImage(msg.file.mime)"
                @click="preview = msg.file"
                class="block mt-1 cursor-zoom-in"
                :title="msg.file.name"
              >
                <img
                  :src="msg.file.url"
                  :alt="msg.file.name"
                  class="max-h-40 max-w-[240px] w-auto rounded-lg object-cover hover:opacity-90 transition"
                  @load="scrollBottom"
                >
              </button>
              <a
                v-else
                :href="msg.file.url"
                :download="msg.file.name"
                class="flex items-center gap-3 mt-1 px-3 py-2 rounded-xl bg-black/15 hover:bg-black/25 transition"
              >
                <span class="text-2xl">📄</span>
                <span class="min-w-0">
                  <span class="block text-sm truncate">{{ msg.file.name }}</span>
                  <span class="block text-xs opacity-70">{{ formatSize(msg.file.size) }} · Download</span>
                </span>
              </a>
            </template>
            <div v-else class="text-sm leading-relaxed whitespace-pre-wrap break-words">{{ msg.content }}</div>
          </div>
        </div>
        <!-- Empty state -->
        <div v-if="messages.length === 0" class="text-center text-slate-500 text-sm pt-16 space-y-4">
          <p>Session started. Share this link to invite others ✨</p>
          <div class="flex gap-2 max-w-md mx-auto">
            <input
              :value="inviteLink"
              readonly
              @focus="($event.target as HTMLInputElement).select()"
              class="flex-1 min-w-0 bg-white/5 border border-white/10 rounded-xl px-3 py-2 text-slate-300 text-base sm:text-xs font-mono outline-none"
            >
            <button
              @click="copyInvite"
              class="shrink-0 px-4 py-2 rounded-xl text-white text-xs font-semibold active:scale-95 transition-all"
              :class="copied ? 'bg-emerald-500/70' : 'gradient-bg hover:opacity-90'"
            >
              {{ copied ? 'Copied' : 'Copy' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Input Bar -->
      <div class="px-3 sm:px-4 pt-3 sm:pt-4 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:pb-4 border-t border-white/10">
        <div v-if="uploading" class="mb-2 px-2 text-xs text-slate-400 flex items-center gap-3">
          <span class="truncate">Uploading {{ uploading.name }}</span>
          <div class="flex-1 h-1 bg-white/10 rounded-full overflow-hidden">
            <div class="h-full gradient-bg transition-all" :style="{ width: uploading.progress + '%' }"></div>
          </div>
          <span>{{ uploading.progress }}%</span>
        </div>
        <div v-else-if="notice" class="mb-2 px-2 text-xs text-red-400">{{ notice }}</div>
        <div class="flex items-center gap-3 bg-white/5 border border-white/10 rounded-2xl px-4 py-2">
          <input ref="fileInput" type="file" multiple class="hidden" @change="onFilePicked">
          <button
            @click="fileInput?.click()"
            :disabled="!!uploading || !connected"
            class="text-slate-400 hover:text-white w-9 h-9 rounded-xl flex items-center justify-center hover:bg-white/10 transition disabled:opacity-40"
            :title="`Share files (max ${MAX_FILE_MB} MB)`"
          >
            📎
          </button>
          <input
            v-model="newMessage"
            @keyup.enter="sendMessage"
            @paste="onPaste"
            type="text"
            maxlength="2000"
            placeholder="Transmit a message..."
            class="flex-1 min-w-0 bg-transparent text-white placeholder-slate-600 outline-none text-base sm:text-sm"
          >
          <button
            @click="sendMessage"
            class="gradient-bg text-white w-9 h-9 rounded-xl flex items-center justify-center hover:opacity-80 active:scale-90 transition-all text-base"
          >
            ↑
          </button>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, nextTick, onMounted, onUnmounted } from 'vue'
import axios from 'axios'
import { copyText } from '../utils/clipboard'

const joined = ref(false)
const currentRoom = ref('')
const joinId = ref('')
const invited = ref(false)
const copied = ref(false)
const nameInput = ref<HTMLInputElement | null>(null)
const baseUrl = import.meta.env.BASE_URL
const inviteLink = computed(() =>
  `${location.origin}${baseUrl}?room=${encodeURIComponent(currentRoom.value)}`)
const newMessage = ref('')
const messageBox = ref<HTMLElement | null>(null)
const socket = ref<WebSocket | null>(null)
const messages = ref<any[]>([])
const myId = ref('')
const token = ref('')
const connected = ref(false)

const MAX_FILE_MB = 10
const INLINE_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref<{ name: string; progress: number } | null>(null)
const notice = ref('')
const dragDepth = ref(0)
const members = ref<{ id: string; name: string }[]>([])
const showMembers = ref(false)
const ACTIVE_ROOM_KEY = 'cc_active_room'
const preview = ref<{ url: string; name: string; size: number } | null>(null)
const onKeydown = (e: KeyboardEvent) => { if (e.key === 'Escape') preview.value = null }

const profile = reactive({
  name: localStorage.getItem('cc_name') || '',
  avatar: 'default',
})

// The server's explanation for a failed request (e.g. a rate limit), if any.
const serverMessage = (err: unknown): string =>
  (axios.isAxiosError(err) && typeof err.response?.data?.message === 'string' && err.response.data.message) || ''

let noticeTimer: ReturnType<typeof setTimeout> | undefined
const flashNotice = (text: string) => {
  notice.value = text
  clearTimeout(noticeTimer)
  noticeTimer = setTimeout(() => { if (notice.value === text) notice.value = '' }, 5000)
}

const createRoom = async () => {
  if (!profile.name.trim()) { alert('Please enter a nickname'); return }
  localStorage.setItem('cc_name', profile.name)
  try {
    const res = await axios.post('/api/rooms')
    enterChat(res.data.id)
  } catch (err) {
    alert(serverMessage(err) || 'Failed to create session, please try again')
  }
}

const joinRoom = async () => {
  if (!profile.name.trim()) { alert('Please enter a nickname'); return }
  const id = parseRoomId(joinId.value)
  if (!id) { alert('Please enter a session ID'); return }
  localStorage.setItem('cc_name', profile.name)
  try {
    await axios.get(`/api/rooms/${encodeURIComponent(id)}`)
  } catch (err) {
    try { sessionStorage.removeItem(ACTIVE_ROOM_KEY) } catch {}
    alert(axios.isAxiosError(err) && err.response?.status === 404
      ? 'Session not found or expired'
      : serverMessage(err) || 'Failed to join session, please try again')
    return
  }
  enterChat(id)
}

// Accepts a bare session ID or a full invite link.
const parseRoomId = (input: string) => {
  const text = input.trim()
  try {
    const room = new URL(text).searchParams.get('room')
    if (room) return room.trim()
  } catch {}
  return text
}

const enterChat = (roomId: string) => {
  currentRoom.value = roomId
  // Keep the address bar shareable and refresh-safe.
  history.replaceState(null, '', `${baseUrl}?room=${encodeURIComponent(roomId)}`)
  // Survives a reload of this tab (but not a new tab), so refresh rejoins.
  try { sessionStorage.setItem(ACTIVE_ROOM_KEY, roomId) } catch {}
  joined.value = true
  initWebSocket(roomId)
}

const initWebSocket = (roomId: string) => {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${protocol}//${location.host}/ws/${roomId}?name=${encodeURIComponent(profile.name)}`
  const ws = new WebSocket(url)
  socket.value = ws

  ws.onopen = () => {
    connected.value = true
    if (reconnecting.value) notice.value = ''
    reconnecting.value = false
    reconnectAttempts = 0
  }
  ws.onclose = () => {
    if (socket.value !== ws) return // we closed it on purpose (Exit)
    connected.value = false
    members.value = []
    scheduleReconnect()
  }
  ws.onmessage = (e) => {
    e.data.split('\n').forEach((line: string) => {
      if (!line.trim()) return
      try {
        const msg = JSON.parse(line)
        if (msg.type === 'error') { flashNotice(msg.content); return }
        if (msg.type === 'welcome') { myId.value = msg.senderId; token.value = msg.token; return }
        if (msg.type === 'presence') {
          // You first, then everyone else alphabetically.
          members.value = [...(Array.isArray(msg.members) ? msg.members : [])].sort((x, y) =>
            x.id === myId.value ? -1 : y.id === myId.value ? 1 : x.name.localeCompare(y.name))
          return
        }
        if (msg.type === 'history') {
          // History may arrive after a few live messages; merge without duplicates.
          const ids = new Set(msg.messages.map((m: any) => m.id))
          messages.value = [...msg.messages, ...messages.value.filter((m) => !ids.has(m.id))]
          scrollBottom()
          return
        }
        if (msg.id && messages.value.some((m) => m.id === msg.id)) return
        messages.value.push(msg)
        scrollBottom()
      } catch {}
    })
  }
}

const isSelf = (msg: any) => !!myId.value && msg.senderId === myId.value

const sendMessage = () => {
  if (!newMessage.value.trim() || socket.value?.readyState !== WebSocket.OPEN) return
  socket.value!.send(JSON.stringify({
    type: 'chat',
    content: newMessage.value.trim(),
  }))
  newMessage.value = ''
}

const isInlineImage = (mime: string) => INLINE_IMAGE_TYPES.includes(mime)

const formatSize = (bytes: number) =>
  bytes < 1024 ? `${bytes} B`
    : bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(1)} KB`
      : `${(bytes / 1024 / 1024).toFixed(1)} MB`

const uploadFiles = async (files: FileList | File[]) => {
  notice.value = ''
  if (uploading.value) return
  for (const file of Array.from(files)) {
    if (!token.value || !connected.value) { notice.value = 'Not connected'; return }
    if (file.size === 0) { notice.value = `${file.name} is empty`; continue }
    if (file.size > MAX_FILE_MB * 1024 * 1024) {
      notice.value = `${file.name} is larger than ${MAX_FILE_MB} MB`
      continue
    }
    const form = new FormData()
    form.append('file', file)
    uploading.value = { name: file.name, progress: 0 }
    try {
      await axios.post(`/api/rooms/${encodeURIComponent(currentRoom.value)}/files`, form, {
        headers: { 'X-Client-Token': token.value },
        onUploadProgress: (e) => {
          if (uploading.value && e.total) uploading.value.progress = Math.round((e.loaded * 100) / e.total)
        },
      })
    } catch (err) {
      notice.value = axios.isAxiosError(err) && err.response?.data?.message
        ? err.response.data.message
        : `Failed to upload ${file.name}`
    } finally {
      uploading.value = null
    }
  }
}

const onFilePicked = (e: Event) => {
  const input = e.target as HTMLInputElement
  if (input.files?.length) uploadFiles(input.files)
  input.value = ''
}

// Pasting an image (e.g. a screenshot) into the message box uploads it.
const onPaste = (e: ClipboardEvent) => {
  const files = e.clipboardData?.files
  if (files?.length) {
    e.preventDefault()
    uploadFiles(files)
  }
}

const onDrop = (e: DragEvent) => {
  dragDepth.value = 0
  if (e.dataTransfer?.files.length) uploadFiles(e.dataTransfer.files)
}

// Automatic reconnect: after a network drop or a server restart, keep the
// conversation on screen and reconnect with backoff (1s, 2s, 4s … 15s).
// History arriving on reconnect is merged by message ID, so nothing doubles.
const reconnecting = ref(false)
let reconnectAttempts = 0
let reconnectTimer: ReturnType<typeof setTimeout> | undefined

const scheduleReconnect = () => {
  clearTimeout(reconnectTimer)
  reconnecting.value = true
  notice.value = 'Connection lost — reconnecting…'
  const delay = Math.min(15000, 1000 * 2 ** reconnectAttempts++)
  reconnectTimer = setTimeout(reconnectNow, delay)
}

const reconnectNow = async () => {
  clearTimeout(reconnectTimer)
  const room = currentRoom.value
  if (!joined.value || !room || connected.value) return
  try {
    await axios.get(`/api/rooms/${encodeURIComponent(room)}`)
  } catch (err) {
    if (axios.isAxiosError(err) && err.response?.status === 404) {
      reconnecting.value = false
      notice.value = 'This session has ended — everyone left and it was deleted.'
      try { sessionStorage.removeItem(ACTIVE_ROOM_KEY) } catch {}
      return
    }
    scheduleReconnect() // server unreachable or rate-limited: try again later
    return
  }
  if (joined.value && currentRoom.value === room && !connected.value) initWebSocket(room)
}

// Reconnect right away when the device comes back online or the tab returns.
const onOnline = () => { if (reconnecting.value) { reconnectAttempts = 0; reconnectNow() } }
const onVisible = () => { if (document.visibilityState === 'visible') onOnline() }

const leaveRoom = () => {
  clearTimeout(reconnectTimer)
  reconnecting.value = false
  reconnectAttempts = 0
  const ws = socket.value
  socket.value = null
  ws?.close()
  connected.value = false
  myId.value = ''
  token.value = ''
  notice.value = ''
  preview.value = null
  members.value = []
  showMembers.value = false
  joined.value = false
  messages.value = []
  currentRoom.value = ''
  invited.value = false
  joinId.value = ''
  history.replaceState(null, '', baseUrl)
  try { sessionStorage.removeItem(ACTIVE_ROOM_KEY) } catch {}
}

const scrollBottom = async () => {
  await nextTick()
  if (messageBox.value) messageBox.value.scrollTop = messageBox.value.scrollHeight
}

const formatTime = (t: any) =>
  new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

const copyInvite = async () => {
  if (await copyText(inviteLink.value)) {
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } else {
    prompt('Copy this invite link:', inviteLink.value)
  }
}

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('online', onOnline)
  document.removeEventListener('visibilitychange', onVisible)
  clearTimeout(reconnectTimer)
})

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('online', onOnline)
  document.addEventListener('visibilitychange', onVisible)
  const queryRoom = new URLSearchParams(location.search).get('room')
  if (queryRoom) {
    joinId.value = queryRoom
    let active = ''
    try { active = sessionStorage.getItem(ACTIVE_ROOM_KEY) || '' } catch {}
    if (active === queryRoom && profile.name.trim()) {
      // Page was reloaded while in this room: rejoin straight away.
      joinRoom()
      return
    }
    invited.value = true
    nameInput.value?.focus()
  }
})
</script>
