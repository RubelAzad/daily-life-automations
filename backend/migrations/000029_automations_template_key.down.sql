ALTER TABLE automations
  DROP INDEX uq_automations_template,
  DROP COLUMN template_key;
