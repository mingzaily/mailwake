import { setNonce } from "get-nonce";

// Radix scroll-lock styles use this shared nonce through react-style-singleton.
export function authorizeComponentStyles() {
  const nonce = document.querySelector<HTMLMetaElement>(
    'meta[name="style-nonce"]',
  )?.content;
  if (nonce) setNonce(nonce);
}
