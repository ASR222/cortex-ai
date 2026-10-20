import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { X, Check } from "lucide-react";
import { useDispatch, useSelector } from "react-redux";
import { createOrder, getPlans, loadRazorpay, verifyPayment } from "../features/billing.api";
import { getMe } from "../features/user.api";
import { setUserData } from "../redux/user.slice";
import { errorInfo } from "../utils/axios";
import useDialog from "../hooks/useDialog";
import { AGENT_LABELS, CREDIT_COST, SPECIALISTS } from "../utils/agents";

const inr = (n) => `₹${n.toLocaleString("en-IN", { minimumFractionDigits: n % 1 ? 2 : 0, maximumFractionDigits: 2 })}`;

export default function BillingDrawer({ open, onClose }) {
  const dispatch = useDispatch();
  const { userData } = useSelector((state) => state.user);
  const [plans, setPlans] = useState([]);
  const [testMode, setTestMode] = useState(false);
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState(null); // { type: "ok" | "error", text }

  const close = () => {
    setNotice(null);
    onClose();
  };
  const sheetRef = useDialog(open, close);

  useEffect(() => {
    if (!open) return;
    getPlans()
      .then((data) => {
        setPlans(data.plans);
        setTestMode(data.testMode);
      })
      .catch((err) => setNotice({ type: "error", text: errorInfo(err).message }));
    getMe()
      .then((u) => dispatch(setUserData(u)))
      .catch(() => {});
  }, [open, dispatch]);

  const handleUpgrade = async (planId) => {
    setBusy(planId);
    setNotice(null);
    try {
      const [Razorpay, data] = await Promise.all([loadRazorpay(), createOrder(planId)]);
      const checkout = new Razorpay({
        key: data.keyId,
        amount: data.order.amount,
        currency: data.order.currency,
        order_id: data.order.id,
        name: "CortexAI",
        description: `${data.plan.name} — ${data.plan.credits} credits`,
        prefill: { name: userData?.name, email: userData?.email },
        theme: { color: "#000000" },
        handler: async (response) => {
          try {
            await verifyPayment(response);
            dispatch(setUserData(await getMe()));
            setNotice({ type: "ok", text: `Payment received — ${data.plan.credits} credits added.` });
          } catch (err) {
            // The Razorpay webhook will still credit the account.
            setNotice({ type: "error", text: `${errorInfo(err).message} If you were charged, credits will arrive shortly.` });
          } finally {
            setBusy("");
          }
        },
        modal: { ondismiss: () => setBusy("") },
      });
      checkout.open();
    } catch (err) {
      setNotice({ type: "error", text: errorInfo(err).message });
      setBusy("");
    }
  };

  const credits = userData?.credits ?? 0;
  const total = Math.max(userData?.totalCredits ?? 0, credits, 1);

  return (
    <AnimatePresence>
      {open && (
          <motion.div
            key="billing-backdrop"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={close}
            className="fixed inset-0 bg-black/70 z-40"
          />
      )}
      {open && (
          <motion.div
            key="billing-sheet"
            ref={sheetRef}
            tabIndex={-1}
            role="dialog"
            aria-modal="true"
            aria-labelledby="billing-title"
            initial={{ x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            transition={{ duration: 0.25, ease: "easeOut" }}
            className="fixed right-0 top-0 z-50 h-dvh w-full max-w-[420px] bg-surface flex flex-col outline-none pb-[env(safe-area-inset-bottom)]"
          >
            <div className="h-14 shrink-0 flex items-center justify-between pl-6 pr-3">
              <h2 id="billing-title" className="text-[16px] font-medium">
                Credits
              </h2>
              <button
                onClick={close}
                aria-label="Close"
                className="w-9 h-9 pointer-coarse:w-11 pointer-coarse:h-11 rounded-full text-faint hover:text-ink hover:bg-hover flex items-center justify-center cursor-pointer"
              >
                <X size={18} />
              </button>
            </div>

            <div className="flex-1 overflow-auto px-6 pb-8">
              <section aria-label="Balance" className="pt-2">
                <p className="flex items-baseline gap-2">
                  <span className="num text-[32px] font-semibold tracking-[-0.02em]">{credits}</span>
                  <span className="text-[15px] text-muted">
                    of <span className="num">{total}</span> credits left
                  </span>
                </p>
                <div className="mt-3 h-2 rounded-full bg-raised overflow-hidden" aria-hidden="true">
                  <div className="h-full rounded-full bg-accent transition-[width] duration-500" style={{ width: `${(credits / total) * 100}%` }} />
                </div>
                <div className="num mt-1.5 flex justify-between text-[12px] text-faint">
                  <span>0</span>
                  <span>{total}</span>
                </div>
                <p className="mt-3 text-[13px] text-faint">
                  {(userData?.plan ?? "free").replace(/^./, (c) => c.toUpperCase())} plan
                </p>
              </section>

              {testMode && (
                <p className="mt-6 rounded-2xl bg-warning-bg p-3.5 text-[13px] leading-[20px] text-warning">
                  Test mode: no real money moves. Pay with UPI ID <b>success@razorpay</b>, or any{" "}
                  <a
                    href="https://razorpay.com/docs/payments/payments/test-card-details/"
                    target="_blank"
                    rel="noreferrer"
                    className="underline underline-offset-2"
                  >
                    Razorpay test card
                  </a>
                  .
                </p>
              )}

              {notice && (
                <p
                  role="status"
                  className={`mt-4 flex gap-2 rounded-2xl p-3.5 text-[13px] leading-[20px] ${
                    notice.type === "ok" ? "bg-hover text-ink" : "bg-negative-bg text-negative"
                  }`}
                >
                  {notice.type === "ok" && <Check size={16} className="shrink-0 mt-0.5 text-accent" />}
                  {notice.text}
                </p>
              )}

              <section aria-labelledby="plans-title" className="mt-8">
                <h3 id="plans-title" className="text-[15px] font-medium mb-3">
                  Buy credits
                </h3>
                <ul className="flex flex-col gap-2">
                  {plans.map((plan) => (
                    <li key={plan.id} className="flex items-center gap-4 rounded-2xl bg-hover p-4">
                      <div className="flex-1 min-w-0">
                        <p className="text-[15px] font-medium">{plan.name}</p>
                        <p className="text-[13px] text-faint mt-0.5">
                          <span className="num">{plan.credits.toLocaleString("en-IN")}</span> credits ·{" "}
                          <span className="num">{plan.validityDays}</span> days
                        </p>
                      </div>
                      <p className="text-right">
                        <span className="num block text-[18px] font-medium">{inr(plan.amount)}</span>
                        <span className="num block text-[12px] text-faint">{inr(plan.amount / plan.credits)} / credit</span>
                      </p>
                      <button
                        disabled={!!busy}
                        onClick={() => handleUpgrade(plan.id)}
                        className="h-8 pointer-coarse:h-11 px-4 rounded-full border border-control text-[13px] hover:bg-raised cursor-pointer transition-colors disabled:opacity-50 disabled:cursor-wait"
                      >
                        {busy === plan.id ? "Opening…" : "Buy"}
                      </button>
                    </li>
                  ))}
                </ul>
              </section>

              <section aria-labelledby="costs-title" className="mt-8">
                <h3 id="costs-title" className="text-[15px] font-medium mb-2">
                  Cost per answer
                </h3>
                <table className="w-full text-[14px]">
                  <thead>
                    <tr className="text-[12px] text-faint">
                      <th className="text-left font-normal pb-1.5">Specialist</th>
                      <th className="text-right font-normal pb-1.5">Credits</th>
                    </tr>
                  </thead>
                  <tbody>
                    {SPECIALISTS.map(({ id }) => (
                      <tr key={id} className="border-t border-rule">
                        <td className="py-2">{AGENT_LABELS[id]}</td>
                        <td className="num py-2 text-right">{CREDIT_COST[id]}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <p className="text-[13px] text-faint mt-3">Failed answers are refunded automatically.</p>
              </section>
            </div>
          </motion.div>
      )}
    </AnimatePresence>
  );
}
