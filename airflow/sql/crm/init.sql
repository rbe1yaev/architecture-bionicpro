-- Эмуляция CRM DB (Битрикс24): клиенты и их протезы.
-- user_id = preferred_username пользователя в Keycloak (realm reports-realm).

CREATE SCHEMA IF NOT EXISTS crm;

CREATE TABLE crm.clients (
    client_id   integer PRIMARY KEY,
    user_id     text    NOT NULL UNIQUE,
    full_name   text    NOT NULL,
    email       text    NOT NULL,
    country     text    NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crm.prostheses (
    prosthesis_id  integer PRIMARY KEY,
    client_id      integer NOT NULL REFERENCES crm.clients (client_id),
    model          text    NOT NULL,
    serial_number  text    NOT NULL UNIQUE,
    installed_at   date    NOT NULL,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

INSERT INTO crm.clients (client_id, user_id, full_name, email, country) VALUES
    (1, 'prothetic1', 'Иван Петров',     'prothetic1@example.com', 'RU'),
    (2, 'prothetic2', 'Мария Соколова',  'prothetic2@example.com', 'RU'),
    (3, 'prothetic3', 'Алексей Смирнов', 'prothetic3@example.com', 'RU');

-- У prothetic1 два протеза: отчёт должен показать оба
INSERT INTO crm.prostheses (prosthesis_id, client_id, model, serial_number, installed_at) VALUES
    (101, 1, 'BionicPRO Hand X2', 'BP-HX2-000101', DATE '2025-03-14'),
    (102, 1, 'BionicPRO Arm A1',  'BP-AA1-000102', DATE '2025-11-02'),
    (201, 2, 'BionicPRO Hand X2', 'BP-HX2-000201', DATE '2025-06-20'),
    (301, 3, 'BionicPRO Leg L3',  'BP-LL3-000301', DATE '2026-01-09');
