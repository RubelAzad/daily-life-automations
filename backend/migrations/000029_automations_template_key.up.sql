-- Automations added from a built-in workflow remember which one, so each
-- can be added once per user and the page can show it as added. They are
-- ordinary automations otherwise: the user can edit or delete them.
ALTER TABLE automations
  ADD COLUMN template_key VARCHAR(40) NULL AFTER user_id,
  ADD UNIQUE KEY uq_automations_template (user_id, template_key);
