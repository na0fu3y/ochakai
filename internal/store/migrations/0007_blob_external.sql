-- Attachment bytes may live in an external blob store (design doc 0011):
-- bytes becomes nullable. NULL means the content lives at blob/<sha256>
-- in the configured GCS bucket; non-NULL rows are migrated (and nulled)
-- at startup when OCHAKAI_GCS_BUCKET is set.
ALTER TABLE blob ALTER COLUMN bytes DROP NOT NULL;
