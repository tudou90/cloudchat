// Client-side encryption for secret messages.
//
// The plaintext is encrypted in the browser with a key derived from both the
// password and a random key carried in the link's #fragment (never sent to the
// server). The server stores only ciphertext plus a hash of an "auth" token
// derived from the same inputs, which it requires before releasing the
// ciphertext, so wrong passwords don't burn the secret.

export const PBKDF2_ITERATIONS = 600_000

const enc = new TextEncoder()
const dec = new TextDecoder()

const randomBytes = (n: number) => crypto.getRandomValues(new Uint8Array(n))

export const toBase64 = (bytes: Uint8Array) => {
  let s = ''
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i])
  return btoa(s)
}

export const fromBase64 = (b64: string) => {
  const s = atob(b64)
  const out = new Uint8Array(s.length)
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i)
  return out
}

const toBase64Url = (bytes: Uint8Array) =>
  toBase64(bytes).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')

const fromBase64Url = (s: string) => {
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/')
  return fromBase64(b64 + '='.repeat((4 - (b64.length % 4)) % 4))
}

export const cryptoAvailable = () => !!globalThis.crypto?.subtle

async function deriveKeys(password: string, salt: Uint8Array<ArrayBuffer>, iterations: number, linkKey: Uint8Array) {
  const pwKey = await crypto.subtle.importKey('raw', enc.encode(password), 'PBKDF2', false, ['deriveBits'])
  const stretched = new Uint8Array(
    await crypto.subtle.deriveBits({ name: 'PBKDF2', hash: 'SHA-256', salt, iterations }, pwKey, 256),
  )
  const ikm = new Uint8Array(stretched.length + linkKey.length)
  ikm.set(stretched)
  ikm.set(linkKey, stretched.length)

  const hkdf = await crypto.subtle.importKey('raw', ikm, 'HKDF', false, ['deriveKey', 'deriveBits'])
  const encKey = await crypto.subtle.deriveKey(
    { name: 'HKDF', hash: 'SHA-256', salt, info: enc.encode('cloudchat-secret-enc-v1') },
    hkdf,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  )
  const auth = new Uint8Array(
    await crypto.subtle.deriveBits(
      { name: 'HKDF', hash: 'SHA-256', salt, info: enc.encode('cloudchat-secret-auth-v1') },
      hkdf,
      256,
    ),
  )
  return { encKey, auth: toBase64(auth) }
}

export async function encryptSecret(plaintext: string, password: string) {
  const salt = randomBytes(16)
  const iv = randomBytes(12)
  const linkKey = randomBytes(32)
  const { encKey, auth } = await deriveKeys(password, salt, PBKDF2_ITERATIONS, linkKey)
  const ciphertext = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, encKey, enc.encode(plaintext)))
  return {
    payload: {
      ciphertext: toBase64(ciphertext),
      iv: toBase64(iv),
      salt: toBase64(salt),
      iterations: PBKDF2_ITERATIONS,
      auth,
    },
    linkKey: toBase64Url(linkKey),
  }
}

// Derives the auth token and decryption key for an existing secret.
// Throws if the link key is malformed.
export async function prepareReveal(password: string, saltB64: string, iterations: number, linkKeyB64Url: string) {
  const linkKey = fromBase64Url(linkKeyB64Url)
  if (linkKey.length !== 32) throw new Error('invalid link key')
  return deriveKeys(password, fromBase64(saltB64), iterations, linkKey)
}

export async function decryptSecret(encKey: CryptoKey, ciphertextB64: string, ivB64: string) {
  const plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: fromBase64(ivB64) }, encKey, fromBase64(ciphertextB64))
  return dec.decode(plain)
}
