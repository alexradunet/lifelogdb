WITH held AS (                                   -- open, live accounts on :day, and the day of the balance that counts
  SELECT a.id, a.name, a.side, a.currency,
         (SELECT b.day FROM balance_values b
           WHERE b.account_id = a.id AND b.day <= :day ORDER BY b.day DESC LIMIT 1) AS as_of
    FROM accounts a
    JOIN entities e ON e.id = a.id AND e.deleted_at IS NULL
   WHERE coalesce(a.opened_day, '0000-01-01') <= :day
     AND coalesce(a.closed_day, '9999-12-31') >= :day
)
SELECT h.name, h.side, h.currency, b.amount, h.as_of,
       CAST(julianday(:day) - julianday(h.as_of) AS INTEGER) AS stale_days,        -- how old the number is
       (CASE h.side WHEN 'asset' THEN 1 ELSE -1 END) * b.amount AS net_minor       -- signed, minor units of h.currency
  FROM held h
  CROSS JOIN balance_values b ON b.account_id = h.id AND b.day = h.as_of   -- CROSS JOIN pins the order: accounts first, then seek
 ORDER BY h.currency, h.side, h.name;
