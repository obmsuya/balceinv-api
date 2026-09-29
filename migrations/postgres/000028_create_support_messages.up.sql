CREATE TABLE support_messages (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    shop_id UUID NULL,
    user_id UUID NOT NULL,
    topic TEXT NOT NULL CHECK (topic IN ('question', 'problem', 'billing', 'idea')),
    message TEXT NOT NULL CHECK (char_length(message) BETWEEN 5 AND 5000),
    contact_email TEXT NULL,
    contact_phone TEXT NULL,
    include_details BOOLEAN NOT NULL DEFAULT TRUE,
    details JSONB NOT NULL DEFAULT '{}',
    screenshot_key TEXT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT support_messages_needs_contact CHECK (contact_email IS NOT NULL OR contact_phone IS NOT NULL),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id)
);

CREATE INDEX idx_support_messages_company_id_user_id_created_at ON support_messages (company_id, user_id, created_at);
CREATE INDEX idx_support_messages_company_id_created_at ON support_messages (company_id, created_at);
CREATE INDEX idx_support_messages_status_next_attempt_at ON support_messages (status, next_attempt_at);
