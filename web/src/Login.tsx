import { useState } from "react";
import { setToken } from "./api";

// Login captures the static bearer token (MVP auth) and stores it.
export function Login({ onAuthed }: { onAuthed: () => void }) {
  const [value, setValue] = useState("");

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!value.trim()) return;
    setToken(value.trim());
    onAuthed();
  };

  return (
    <div className="login">
      <form className="login__card" onSubmit={submit}>
        <h1 className="login__title">pglockr</h1>
        <p className="login__sub">Enter the access token to connect.</p>
        <input
          className="login__input"
          type="password"
          autoFocus
          placeholder="access token"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <button className="btn btn--primary" type="submit">
          Connect
        </button>
      </form>
    </div>
  );
}
