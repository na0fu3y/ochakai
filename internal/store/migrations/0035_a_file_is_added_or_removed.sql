-- attach/detach retired the way the concept they name already did (0046
-- §2.1, 0064 §6): a directory holds files, and what happens to one is
-- adding it or removing it. Rewrite the code, not the fact: who touched
-- which file, and when, is untouched — only the name the product gave
-- the act changes.
UPDATE knowledge_revision SET change = 'add_file' WHERE change = 'attach';
UPDATE knowledge_revision SET change = 'remove_file' WHERE change = 'detach';
