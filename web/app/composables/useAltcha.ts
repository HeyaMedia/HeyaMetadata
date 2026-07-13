// Altcha-style proof-of-work captcha solver (self-hosted, no third party). The
// backend issues a signed challenge; we brute-force the number whose
// SHA-256(salt + number) matches the challenge hash, then submit a base64
// solution the backend verifies (recompute + HMAC signature + single-use).
//
// Matches the standard Altcha payload so it interoperates with the Go
// altcha-lib on the backend. Degrades gracefully: if the challenge endpoint is
// absent (before the backend ships it), obtain() returns null and the caller
// proceeds without a token.
const CHALLENGE_URL = '/api/v2/auth/challenge'
// Safety ceiling so a misconfigured difficulty can't spin forever on the main
// thread; the backend is expected to issue a modest maxnumber (~50k).
const MAX_ITERATIONS = 2_000_000

interface Challenge {
  algorithm?: string
  challenge: string
  salt: string
  maxnumber?: number
  signature: string
}

async function sha256Hex(input: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(input))
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, '0')).join('')
}

// Awaiting each async digest naturally yields to the event loop, so the solve
// stays off the critical path and never freezes the UI.
async function solve(challenge: Challenge): Promise<number | null> {
  const max = Math.min(challenge.maxnumber ?? 100_000, MAX_ITERATIONS)
  for (let number = 0; number <= max; number++) {
    if (await sha256Hex(challenge.salt + number) === challenge.challenge) return number
  }
  return null
}

export function useAltcha() {
  /** Fetch + solve a challenge, returning the base64 solution, or null if the
   *  captcha isn't available (endpoint absent / solve failed). */
  async function obtain(): Promise<string | null> {
    let challenge: Challenge
    try {
      const response = await fetch(CHALLENGE_URL, { credentials: 'same-origin' })
      if (!response.ok) return null
      challenge = await response.json()
    } catch {
      return null
    }
    if (!challenge?.challenge || !challenge?.salt || !challenge?.signature) return null

    const number = await solve(challenge)
    if (number == null) return null

    const payload = {
      algorithm: challenge.algorithm ?? 'SHA-256',
      challenge: challenge.challenge,
      number,
      salt: challenge.salt,
      signature: challenge.signature,
    }
    return btoa(JSON.stringify(payload))
  }

  return { obtain }
}
