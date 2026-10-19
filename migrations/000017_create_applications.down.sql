DROP TABLE gogi_credentials;
ALTER TABLE gogi_registered_llms DROP COLUMN served_name;
ALTER TABLE gogi_prompts DROP COLUMN application;
DROP TABLE gogi_application_keys;
DROP TABLE gogi_applications;
