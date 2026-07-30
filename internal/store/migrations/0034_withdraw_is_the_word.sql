-- One ruling, one word. The wire spells taking back a rejection
-- `withdrawn` (design doc 0055 §3.2); the revision log spelled the same
-- act `unreject`, so a caller who sent one word read the other back out
-- of the history it wrote. Rewrite the code, not the fact: who withdrew
-- what, and when, is untouched — only the name the product gave the act
-- changes, and leaving old rows behind would keep both spellings alive
-- forever in the one place they sit side by side.
UPDATE knowledge_revision SET change = 'withdraw' WHERE change = 'unreject';
