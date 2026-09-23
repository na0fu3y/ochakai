// The other end of signInForBigQuery (sql.js): Google's consent screen
// redirects here with the token in the fragment. It goes to the opener,
// on this origin and nowhere else, and this window closes. Nothing is
// stored: the token lives in the page that asked for it, in memory.

const params = new URLSearchParams(location.hash.slice(1));
const message = {
  kind: 'ochakai-oauth',
  state: params.get('state') || '',
  token: params.get('access_token') || '',
  expiresIn: Number(params.get('expires_in') || 0),
  error: params.get('error') || '',
};
history.replaceState(null, '', location.pathname);
if (window.opener) window.opener.postMessage(message, location.origin);
window.close();
