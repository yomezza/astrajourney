CREATE TABLE IF NOT EXISTS expedition_command_receipts (
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id uuid NOT NULL,
    digest text NOT NULL,
    ship_id bigint NOT NULL,
    action text NOT NULL CHECK (action IN ('buy', 'sell')),
    PRIMARY KEY (user_id, request_id)
);
