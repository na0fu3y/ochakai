-- A deployment with no OCHAKAI_GCS_BUCKET keeps file bytes here
-- (decision 0156). Without them it held markdown concepts only, so a
-- bundle carrying an attester's .py or a diagram could not round-trip
-- through a local, CI or off-Google-Cloud deployment — and every bundle
-- the OKF repository publishes carries one.
--
-- A table of its own rather than the bytea column 0009 dropped from
-- blob: `blob` is the ledger of hashes the sweep reads, whichever store
-- holds the bytes, and a deployment that later names a bucket keeps
-- reading what is here while new bytes go to GCS.
CREATE TABLE IF NOT EXISTS blob_bytes (
    sha256 text  NOT NULL PRIMARY KEY,
    bytes  bytea NOT NULL
);
