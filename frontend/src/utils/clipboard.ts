// Copies text to the clipboard. navigator.clipboard only exists in secure
// contexts (HTTPS/localhost), so fall back to execCommand over plain HTTP.
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {}
  const el = document.createElement('textarea')
  el.value = text
  el.setAttribute('readonly', '')
  el.style.position = 'fixed'
  el.style.opacity = '0'
  document.body.appendChild(el)
  el.select()
  let ok = false
  try { ok = document.execCommand('copy') } catch {}
  document.body.removeChild(el)
  return ok
}
