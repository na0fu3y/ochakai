-- The MCP OAuth connector service is retired (design doc 0012): no
-- surface reads or writes these tables anymore, and leaving credential
-- stores (even hash-only ones) in every database contradicts the
-- smallest-possible-surface posture. Fresh databases never had them —
-- 0006_oauth_connector.sql was removed together with the feature —
-- hence IF EXISTS.
DROP TABLE IF EXISTS oauth_grant;
DROP TABLE IF EXISTS oauth_code;
DROP TABLE IF EXISTS oauth_pending;
