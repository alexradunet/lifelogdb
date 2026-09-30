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
       CAST(round((CASE h.side WHEN 'asset' THEN 1 ELSE -1 END) * b.amount
            * CASE WHEN h.currency = :base THEN 1.0
                   WHEN h.currency < :base THEN (SELECT r.rate FROM fx_rates r
                        WHERE r.from_ccy = h.currency AND r.to_ccy = :base AND r.day <= :day ORDER BY r.day DESC LIMIT 1)
                   ELSE 1.0 / (SELECT r.rate FROM fx_rates r
                        WHERE r.from_ccy = :base AND r.to_ccy = h.currency AND r.day <= :day ORDER BY r.day DESC LIMIT 1)
              END
            * (SELECT subunits FROM currencies WHERE code = :base) * 1.0
            / (SELECT subunits FROM currencies WHERE code = h.currency)) AS INTEGER) AS net_base_minor  -- NULL = no FX rate
  FROM held h
  JOIN balance_values b ON b.account_id = h.id AND b.day = h.as_of
 ORDER BY h.side, h.name;
