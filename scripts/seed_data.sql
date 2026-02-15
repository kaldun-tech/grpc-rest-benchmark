-- Seed 10,000 accounts with random balances
INSERT INTO accounts (account_id, balance_tinybar, updated_at)
SELECT
    '0.0.' || id,
    (RANDOM() * 100000000000)::BIGINT,  -- Random balance 0-100B tinybar
    NOW() - (RANDOM() * INTERVAL '30 days')
FROM generate_series(100000, 109999) AS id;

-- Seed 100,000 transactions over 24 hours
WITH tx_data AS (
    SELECT
        id,
        NOW() - (RANDOM() * INTERVAL '24 hours') as tx_time,
        CASE
            WHEN RANDOM() < 0.6 THEN 'transfer'
            WHEN RANDOM() < 0.9 THEN 'vesting_release'
            ELSE 'contract_call'
        END as tx_type,
        (RANDOM() * 10000000000)::BIGINT as amount
    FROM generate_series(1, 100000) AS id
)
INSERT INTO transactions (tx_id, from_account, to_account, amount_tinybar, tx_type, timestamp)
SELECT
    '0.0.' || (100000 + (id % 10000)) || '@' || EXTRACT(EPOCH FROM tx_time)::BIGINT,
    '0.0.' || (100000 + (RANDOM() * 10000)::INT),
    '0.0.' || (100000 + (RANDOM() * 10000)::INT),
    amount,
    tx_type,
    tx_time
FROM tx_data;

-- Create indexes after bulk insert for performance
REINDEX TABLE accounts;
REINDEX TABLE transactions;

-- Verify data
SELECT 'Accounts created:', COUNT(*) FROM accounts;
SELECT 'Transactions created:', COUNT(*) FROM transactions;
SELECT 'Transaction types:', tx_type, COUNT(*) FROM transactions GROUP BY tx_type;
