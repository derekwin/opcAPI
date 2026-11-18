"""Expire one trial account, keeping its key records and usage counts."""

import sqlite3
import sys
import time


def expire(database, user_id, username):
    with sqlite3.connect(database, timeout=10) as db:
        user = db.execute(
            "SELECT id FROM users WHERE id=? AND name=? AND admin=0",
            (user_id, username),
        ).fetchone()
        if user is None:
            return

        db.execute(
            "UPDATE api_keys SET revoked=COALESCE(revoked,?) WHERE owner=?",
            (int(time.time()), user[0]),
        )
        db.execute("DELETE FROM sessions WHERE user_id=?", user)
        db.execute("DELETE FROM external_users WHERE user_id=?", user)
        db.execute(
            "UPDATE users SET salt=NULL,password=NULL,invite=NULL,invite_until=NULL WHERE id=?",
            user,
        )


if __name__ == "__main__":
    if len(sys.argv) != 4:
        sys.exit("Usage: expire-trial.py DATABASE USER_ID USERNAME")
    expire(*sys.argv[1:])
