/** CortexAI's mark: a C of two folds, and the dot the router sends a message to. */
export default function Mark({ size = 20 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M18.4 7.2A8 8 0 1 0 18.4 16.8" stroke="var(--color-accent)" strokeWidth="2.4" strokeLinecap="round" />
      <path d="M15 9.6A3.6 3.6 0 1 0 15 14.4" stroke="var(--color-accent)" strokeWidth="2.4" strokeLinecap="round" />
      <circle cx="19.2" cy="12" r="1.7" fill="var(--color-accent)" />
    </svg>
  );
}
