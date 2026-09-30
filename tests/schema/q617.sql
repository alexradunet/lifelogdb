WITH RECURSIVE month_ends(day) AS (
  SELECT date(:from_day, 'start of month', '+1 month', '-1 day')
  UNION ALL
  SELECT date(day, 'start of month', '+2 month', '-1 day') FROM month_ends
   WHERE day < date(:to_day, 'start of month', '+1 month', '-1 day')
),
held AS (                                        -- every open, live account on every month-end, with its latest balance
  SELECT m.day, a.side, a.currency,
         (SELECT b.amount FROM balance_values b
           WHERE b.account_id = a.id AND b.day <= m.day ORDER BY b.day DESC LIMIT 1) AS amount
    FROM month_ends m
    JOIN accounts a ON coalesce(a.opened_day, '0000-01-01') <= m.day
                   AND coalesce(a.closed_day, '9999-12-31') >= m.day
    JOIN entities e ON e.id = a.id AND e.deleted_at IS NULL
)
SELECT day,
       CAST(round(sum(net)) AS INTEGER) AS net_worth_minor,   -- reporting currency, minor units
       count(*)                          AS accounts,          -- accounts that have a balance by then
       sum(net IS NULL)                  AS unconverted        -- > 0: an FX rate is missing and the total understates
  FROM (
    SELECT h.day,
           (CASE h.side WHEN 'asset' THEN 1 ELSE -1 END) * h.amount
           * CASE WHEN h.currency = :base THEN 1.0
                  WHEN h.currency < :base THEN (SELECT r.rate FROM fx_rates r
                       WHERE r.from_ccy = h.currency AND r.to_ccy = :base AND r.day <= h.day ORDER BY r.day DESC LIMIT 1)
                  ELSE 1.0 / (SELECT r.rate FROM fx_rates r
                       WHERE r.from_ccy = :base AND r.to_ccy = h.currency AND r.day <= h.day ORDER BY r.day DESC LIMIT 1)
             END
           * (SELECT subunits FROM currencies WHERE code = :base) * 1.0
           / (SELECT subunits FROM currencies WHERE code = h.currency) AS net
      FROM held h
     WHERE h.amount IS NOT NULL
  )
 GROUP BY day
 ORDER BY day;
