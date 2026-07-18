-- Attachment bytes live only in GCS now (design doc 0013): the bytea
-- column of design doc 0008 is retired. The startup order guarantees the
-- backfill (MigrateBlobsOut) ran before this migration when a bucket is
-- configured; if inline bytes remain anyway — a GCS-less instance with
-- legacy attachments — dropping the column would destroy them, so fail
-- the migration with instructions instead.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'blob' AND column_name = 'bytes') THEN
        IF EXISTS (SELECT 1 FROM blob WHERE bytes IS NOT NULL) THEN
            RAISE EXCEPTION 'attachment bytes are still inline in PostgreSQL; '
                'set OCHAKAI_GCS_BUCKET and restart so they migrate to GCS '
                'before the bytea column can be dropped (design doc 0013)';
        END IF;
        ALTER TABLE blob DROP COLUMN bytes;
    END IF;
END $$;
