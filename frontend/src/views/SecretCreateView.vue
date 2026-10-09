<template>
  <div class="min-h-screen flex items-center justify-center px-4 py-10 animate-slide-up">
    <div class="w-full max-w-lg">
      <div class="text-center mb-8">
        <div class="text-6xl mb-4 select-none">🔥</div>
        <h1 class="text-4xl font-semibold gradient-text mb-2 tracking-tight">Secret Message</h1>
        <p class="text-slate-400 text-sm">Encrypted in your browser. Readable once, then gone.</p>
      </div>

      <!-- Compose -->
      <div v-if="!link" class="glass rounded-3xl p-6 sm:p-8 space-y-5">
        <div>
          <textarea
            v-model="content"
            :maxlength="MAX_LENGTH"
            rows="6"
            placeholder="Write your secret..."
            class="w-full bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-white placeholder-slate-500 outline-none focus:border-sky-400/50 focus:ring-1 focus:ring-sky-400/30 transition resize-none"
          ></textarea>
          <div class="text-right text-xs text-slate-600 mt-1">{{ content.length }} / {{ MAX_LENGTH }}</div>
        </div>

        <input
          v-model="password"
          type="password"
          autocomplete="new-password"
          placeholder="Password"
          class="w-full bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-white placeholder-slate-500 outline-none focus:border-sky-400/50 focus:ring-1 focus:ring-sky-400/30 transition"
        >

        <div>
          <div class="text-xs text-slate-500 mb-2">Destroy after</div>
          <div class="grid grid-cols-3 gap-2">
            <button
              v-for="opt in TTL_OPTIONS"
              :key="opt.value"
              @click="ttl = opt.value"
              class="py-2.5 rounded-xl border text-sm transition"
              :class="ttl === opt.value
                ? 'border-sky-400/60 bg-sky-400/10 text-white'
                : 'border-white/10 bg-white/5 text-slate-400 hover:bg-white/10'"
            >
              {{ opt.label }}
            </button>
          </div>
        </div>

        <p v-if="error" class="text-sm text-red-400">{{ error }}</p>

        <button
          @click="create"
          :disabled="busy"
          class="w-full gradient-bg text-white font-semibold py-3 rounded-xl hover:opacity-90 active:scale-95 transition-all shadow-lg shadow-sky-500/20 disabled:opacity-50 disabled:active:scale-100"
        >
          {{ busy ? 'Encrypting...' : 'Create Secret Link' }}
        </button>
        <p class="text-xs text-slate-600 text-center">
          By creating a note you agree to our
          <a href="/terms" class="underline hover:text-slate-400">Terms</a> and
          <a href="/privacy" class="underline hover:text-slate-400">Privacy Policy</a>.
        </p>
      </div>

      <!-- Result -->
      <div v-else class="glass rounded-3xl p-6 sm:p-8 space-y-5 animate-fade-in">
        <div class="text-sm text-slate-300">Your secret link is ready:</div>
        <div class="flex gap-2">
          <input
            :value="link"
            readonly
            @focus="($event.target as HTMLInputElement).select()"
            class="flex-1 min-w-0 bg-white/5 border border-white/10 rounded-xl px-4 py-3 text-slate-200 text-sm font-mono outline-none"
          >
          <button
            @click="copyLink"
            class="px-4 py-3 bg-white/5 border border-white/10 rounded-xl text-white text-sm font-semibold hover:bg-white/10 active:scale-95 transition-all"
          >
            {{ copied ? 'Copied' : 'Copy' }}
          </button>
        </div>
        <ul class="text-xs text-slate-500 space-y-1.5 list-disc pl-4">
          <li>The link can be opened <span class="text-slate-300">only once</span>.</li>
          <li>Expires {{ expiresText }} if nobody reads it.</li>
          <li>Send the password through a <span class="text-slate-300">different channel</span> than the link.</li>
          <li>{{ MAX_ATTEMPTS }} wrong passwords destroy the secret.</li>
        </ul>
        <button
          @click="reset"
          class="w-full py-3 rounded-xl border border-white/10 text-slate-300 hover:bg-white/5 transition text-sm"
        >
          Create another
        </button>
      </div>

      <div class="text-center mt-6 space-x-4">
        <a href="/" class="text-sm text-slate-500 hover:text-slate-300 transition">← Home</a>
        <a :href="baseUrl" class="text-sm text-slate-500 hover:text-slate-300 transition">Go to chat</a>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import axios from 'axios'
import { copyText } from '../utils/clipboard'
import { cryptoAvailable, encryptSecret } from '../utils/secretCrypto'

const MAX_LENGTH = 10000
const MAX_ATTEMPTS = 5
const TTL_OPTIONS = [
  { value: '1d', label: '1 day' },
  { value: '7d', label: '1 week' },
  { value: '30d', label: '1 month' },
]

const baseUrl = import.meta.env.BASE_URL
const content = ref('')
const password = ref('')
const ttl = ref('1d')
const busy = ref(false)
const error = ref('')
const link = ref('')
const expiresText = ref('')
const copied = ref(false)

const create = async () => {
  error.value = ''
  if (!content.value.trim()) { error.value = 'Please write a message'; return }
  if (!password.value) { error.value = 'Please set a password'; return }
  if (!cryptoAvailable()) { error.value = 'Encryption requires HTTPS (or localhost)'; return }

  busy.value = true
  try {
    const { payload, linkKey } = await encryptSecret(content.value, password.value)
    const res = await axios.post('/api/secrets', { ...payload, ttl: ttl.value })
    link.value = `${location.origin}${baseUrl}secret/${res.data.id}#${linkKey}`
    expiresText.value = new Date(res.data.expiresAt).toLocaleString()
    content.value = ''
    password.value = ''
  } catch (err) {
    error.value = axios.isAxiosError(err) && err.response?.data?.message
      ? err.response.data.message
      : 'Failed to create secret, please try again'
  } finally {
    busy.value = false
  }
}

const copyLink = async () => {
  if (!(await copyText(link.value))) return
  copied.value = true
  setTimeout(() => { copied.value = false }, 1500)
}

const reset = () => {
  link.value = ''
  copied.value = false
}
</script>
