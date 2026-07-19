---
type: Semantic Model
title: sales_analytics
description: Sales star schema (example)
spec:
  name: sales_analytics
  description: Sales star schema (example)
  datasets:
    - name: orders
      source: myproject.shop.orders
      primary_key: [order_id]
      description: One row per order.
      fields:
        - name: amount
          description: Order total in JPY.
          expression:
            - dialect: ANSI_SQL
              expression: total_price
        - name: ordered_at
          expression:
            - dialect: ANSI_SQL
              expression: created_at
          dimension:
            is_time: true
    - name: customers
      source: myproject.shop.customers
      primary_key: [id]
      description: One row per customer.
      fields:
        - name: region
          expression:
            - dialect: ANSI_SQL
              expression: region_code
  relationships:
    - name: orders_to_customers
      from: orders
      to: customers
      from_columns: [customer_id]
      to_columns: [id]
  metrics:
    - name: revenue
      description: Total revenue across all orders.
      expression:
        - dialect: ANSI_SQL
          expression: SUM(orders.amount)
      ai_context:
        synonyms: ["売上", "total sales"]
    - name: avg_order_value
      description: Average order value per order.
      expression:
        - dialect: ANSI_SQL
          expression: SUM(orders.amount) / COUNT(DISTINCT orders.order_id)
      ai_context:
        synonyms: ["AOV", "平均注文単価"]
---

Example Apache Ossie semantic model for ochakai, as a `models` knowledge
entry (design doc 0018): the model object lives verbatim in the `spec`
frontmatter key and is validated when the entry is written.

Register it:

```sh
ochakai create models/sales-analytics -f examples/semantic-model.md
```

Then create a `metrics/<name>` entry per metric — `attrs.model` naming
this entry's id, `attrs.expression` holding the expression — so
`compile_sql` resolves the model and the definitions are searchable.
An agent connected to ochakai can do this from the spec; asking it to
"register examples/semantic-model.md and its metrics" is enough.
