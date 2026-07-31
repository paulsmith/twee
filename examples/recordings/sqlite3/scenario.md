# rlwrap sqlite3 inventory check

- **App:** rlwrap sqlite3
- **Persona:** Priya, an operations coordinator preparing a replenishment request.
- **Mission:** Identify below-reorder stock, record a confirmed incoming quantity, and verify the updated item.
- **Journey:** Priya opens the inventory database with `rlwrap sqlite3`, enables readable results, queries low stock, updates the USB-C cable count, and verifies it.
- **Value demonstrated:** the SQLite prompt supports a compact investigate-and-correct loop, while rlwrap provides familiar interactive line editing and history.
