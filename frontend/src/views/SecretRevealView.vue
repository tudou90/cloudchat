<template>
  <div class="min-h-screen flex items-center justify-center px-4 py-10 animate-slide-up">
    <div class="w-full max-w-lg">
      <div class="text-center mb-8">
        <div class="text-6xl mb-4 select-none">{{ plaintext ? '🔓' : '🔒' }}</div>
        <h1 class="text-4xl font-semibold gradient-text mb-2 tracking-tight">Secret Message</h1>
      </div>

      <div class="glass rounded-3xl p-6 sm:p-8 space-y-5">
        <!-- Loading -->
        <p v-if="state === 'loading'" class="text-center text-slate-500 text-sm">Loading...</p>

        <!-- Unavailable -->
        <div v-else-if="state === 'unavailable'" class="text-center space-y-2">
          <p class="text-slate-200">{{ unavailableMessage }}</p>
          <p class="text-xs text-slate-500">Secrets can be read only once and expire automatically.</p>
        </div>

        <!-- Password prompt -->
        <template v-else-if="state === 'locked'">
          <p class="text-sm text-slate-300">
            Someone sent you a secret. It will be <span class="text-white font-semibold">destroyed</span>
            right after you read it.
          </p>
          <input
            v-model="password"
            @keyup.enter="reveal"
            type="password"
            autocomplete="off"
            placeholder="Password"
            class="w-full bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-white placeholder-slate-500 outline-none focus:border-sky-400/50 focus:ring-1 focus:ring-sky-400/30 transition"
          >
          <p v-if="error" class="text-sm text-red-400">{{ error }}</p>
          <p v-else class="text-xs text-slate-500">
            {{ attemptsLeft }} attempt{{ attemptsLeft === 1 ? '' : 's' }} left · expires {{ expiresText }}
          </p>
          <button
            @click="reveal"
            :disabled="busy"
            class="w-full gradient-bg text-white font-semibold py-3 rounded-xl hover:opacity-90 active:scale-95 transition-all shadow-lg shadow-sky-500/20 disabled:opacity-50 disabled:active:scale-100"
          >
            {{ busy ? 'Decrypting...' : 'Reveal Secret' }}
          </button>
        </template>

        <!-- Revealed -->
        <template v-else-if="state === 'revealed'">
          <div class="bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-slate-100 text-sm leading-relaxed whitespace-pre-wrap break-words max-h-[50vh] overflow-y-auto">{{ plaintext }}</div>
          <div class="flex items-center justify-between gap-3">
            <p class="text-xs text-slate-500">This secret has been deleted from the server. Copy it now if you need it.</p>
            <button
              @click="copyPlaintext"
              class="shrink-0 px-4 py-2 bg-white/5 border border-white/10 rounded-xl text-white text-sm font-semibold hover:bg-white/10 active:scale-95 transition-all"
            >
              {{ copied ? 'Copied' : 'Copy' }}
            </button>
          </div>
        </template>
      </div>

      <div class="text-center mt-6 space-x-4">
        <a href="/" class="text-sm text-slate-500 hover:text-slate-300 transition">← Home</a>
        <a :href="`${baseUrl}secret`" class="text-sm text-slate-500 hover:text-slate-300 transition">Send a secret</a>
        <a :href="baseUrl" class="text-sm text-slate-500 hover:text-slate-300 transition">Go to chat</a>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { copyText } from '../utils/clipboard'
import { cryptoAvailable, decryptSecret, prepareReveal } from '../utils/secretCrypto'

type State = 'loading' | 'unavailable' | 'locked' | 'revealed'

const baseUrl = import.meta.env.BASE_URL
const id = location.pathname.replace(/\/+$/, '').split('/').pop() || ''
const linkKey = location.hash.slice(1)

const state = ref<State>('loading')
const unavailableMessage = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)
const attemptsLeft = ref(0)
const expiresText = ref('')
const plaintext = ref('')
const copied = ref(false)
let meta: { salt: string; iterations: number } | null = null

const unavailable = (msg: string) => {
  unavailableMessage.value = msg
  state.value = 'unavailable'
}

onMounted(async () => {
  if (!linkKey) return unavailable('This link is incomplete. Make sure you copied the whole link.')
  if (!cryptoAvailable()) return unavailable('Decryption requires HTTPS (or localhost).')
  try {
    const res = await axios.get(`/api/secrets/${encodeURIComponent(id)}`)
    meta = res.data
    attemptsLeft.value = res.data.attemptsLeft
    expiresText.value = new Date(res.data.expiresAt).toLocaleString()
    state.value = 'locked'
  } catch (err) {
    unavailable(axios.isAxiosError(err) && err.response?.status === 404
      ? 'This secret does not exist, was already read, or has expired.'
      : (axios.isAxiosError(err) && err.response?.data?.message) || 'Failed to load the secret, please try again.')
  }
})

const reveal = async () => {
  if (!password.value || !meta || busy.value) return
  error.value = ''
  busy.value = true
  try {
    let keys
    try {
      keys = await prepareReveal(password.value, meta.salt, meta.iterations, linkKey)
    } catch {
      return unavailable('This link is malformed. Make sure you copied the whole link.')
    }
    const res = await axios.post(`/api/secrets/${encodeURIComponent(id)}/reveal`, { auth: keys.auth })
    try {
      plaintext.value = await decryptSecret(keys.encKey, res.data.ciphertext, res.data.iv)
    } catch {
      return unavailable('The secret could not be decrypted. It has been deleted.')
    }
    state.value = 'revealed'
    password.value = ''
    // Drop the key from the address bar and history.
    history.replaceState(null, '', location.pathname)
  } catch (err) {
    const resp = axios.isAxiosError(err) ? err.response : undefined
    const status = resp?.status
    if (status === 403) {
      attemptsLeft.value = resp?.data?.attemptsLeft ?? 0
      error.value = `Wrong password. ${attemptsLeft.value} attempt${attemptsLeft.value === 1 ? '' : 's'} left.`
    } else if (status === 410) {
      unavailable('Too many wrong attempts. The secret has been destroyed.')
    } else if (status === 404) {
      unavailable('This secret does not exist, was already read, or has expired.')
    } else {
      error.value = resp?.data?.message || 'Something went wrong, please try again.'
    }
  } finally {
    busy.value = false
  }
}

const copyPlaintext = async () => {
  if (!(await copyText(plaintext.value))) return
  copied.value = true
  setTimeout(() => { copied.value = false }, 1500)
}
</script>
