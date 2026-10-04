-- The chat's consent per tenant (docs/adr/0076): whether the chat may send the
-- tenant's data to a provider the operator does not declare inside the
-- installation's trust boundary, and to which. The consent names the provider
-- it was given to — its wire format, the host of its URL, its model — and a
-- consent given to another provider is none: an operator who points the chat
-- elsewhere does not carry a tenant's yes along. Off until an administrator
-- switches it on; a provider inside needs no consent. The columns only add to
-- what the previous release reads (docs/adr/0028), and the tenants table's
-- policies hold for them as for every other setting (docs/adr/0021).
ALTER TABLE tenants
    ADD COLUMN chat_external_allowed  boolean NOT NULL DEFAULT false,
    ADD COLUMN chat_external_provider text CHECK (length(chat_external_provider) <= 400),
    ADD CONSTRAINT tenants_chat_consent_names_its_provider
        CHECK (NOT chat_external_allowed OR chat_external_provider IS NOT NULL);

DO $$
DECLARE
    runtime text := current_setting('cowork.runtime_role');
BEGIN
    EXECUTE format('GRANT UPDATE (chat_external_allowed, chat_external_provider) ON tenants TO %I', runtime);
END
$$;
