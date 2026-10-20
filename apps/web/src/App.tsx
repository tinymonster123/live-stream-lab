import { BroadcastPanel } from "./components/BroadcastPanel";
import { MeetingPanel } from "./components/MeetingPanel";

export function App() {
  return (
    <main>
      <h1>Live Stream Lab</h1>
      <section>
        <h2>Infrastructure status</h2>
        <p>
          SRS / Redis / Caddy: <strong data-testid="infra-status">unknown</strong>
        </p>
      </section>
      <BroadcastPanel />
      <MeetingPanel />
      <p>
        Control plane: <strong data-testid="control-plane-status">not connected</strong>
      </p>
    </main>
  );
}
