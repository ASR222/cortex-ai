import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { X, Crown, Zap, CheckCircle2, FlaskConical } from "lucide-react";
import { useDispatch, useSelector } from "react-redux";
import { createOrder, getPlans, loadRazorpay, verifyPayment } from "../features/billing.api";
import { getMe } from "../features/user.api";
import { setUserData } from "../redux/user.slice";
import { errorInfo } from "../utils/axios";

const COSTS = [
  ["Chat", 1],
  ["Document Q&A", 2],
  ["Web search", 3],
  ["Vision", 3],
  ["Coding", 5],
  ["PDF / Slides / Image", 5],
];

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
        theme: { color: "#4F46E5" },
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

  const pct = Math.min(100, ((userData?.credits || 0) / Math.max(userData?.totalCredits || 1, 1)) * 100);

  return (
    <AnimatePresence>
      {open && (
        <>
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 0.5 }}
            exit={{ opacity: 0 }}
            onClick={close}
            className="fixed inset-0 bg-black z-40"
          />
          <motion.div
            initial={{ x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            transition={{ duration: 0.25 }}
            className="fixed right-0 top-0 z-50 h-screen w-full max-w-[380px] bg-[#0f1117] border-l border-white/10 shadow-2xl flex flex-col"
          >
            <div className="flex items-center justify-between p-5 border-b border-white/10">
              <div>
                <h2 className="text-white text-lg font-semibold">Billing</h2>
                <p className="text-slate-400 text-sm">Plans & credits</p>
              </div>
              <button
                onClick={close}
                className="w-9 h-9 rounded-lg bg-white/5 hover:bg-white/10 flex items-center justify-center cursor-pointer"
              >
                <X size={18} className="text-slate-300" />
              </button>
            </div>

            <div className="flex-1 overflow-auto">
              <div className="p-5 space-y-4">
                {testMode && (
                  <div className="flex gap-2.5 rounded-xl border border-amber-500/25 bg-amber-500/[0.07] p-3 text-[12px] text-amber-200/90 leading-relaxed">
                    <FlaskConical size={16} className="shrink-0 mt-0.5 text-amber-400" />
                    <span>
                      Test mode — no real money moves. Pay with UPI ID <b>success@razorpay</b>, or any{" "}
                      <a
                        href="https://razorpay.com/docs/payments/payments/test-card-details/"
                        target="_blank"
                        rel="noreferrer"
                        className="underline"
                      >
                        Razorpay test card
                      </a>
                      .
                    </span>
                  </div>
                )}

                <div className="rounded-xl bg-white/[0.04] border border-white/10 p-4">
                  <div className="flex justify-between items-center">
                    <div>
                      <p className="text-slate-400 text-sm">Current plan</p>
                      <h3 className="text-white text-xl font-bold capitalize">{userData?.plan ?? "free"}</h3>
                    </div>
                    <Crown className="text-yellow-400" />
                  </div>
                  <div className="mt-5">
                    <div className="flex justify-between text-xs text-slate-400 mb-2">
                      <span>Credits</span>
                      <span>
                        {userData?.credits ?? 0} / {userData?.totalCredits ?? 0}
                      </span>
                    </div>
                    <div className="h-2 rounded-full bg-white/10 overflow-hidden">
                      <div className="h-full bg-indigo-500 transition-all duration-500" style={{ width: `${pct}%` }} />
                    </div>
                  </div>
                </div>

                {notice && (
                  <div
                    className={`flex gap-2 rounded-xl p-3 text-[12.5px] ${
                      notice.type === "ok"
                        ? "bg-emerald-500/10 text-emerald-300 border border-emerald-500/20"
                        : "bg-red-500/10 text-red-300 border border-red-500/20"
                    }`}
                  >
                    {notice.type === "ok" && <CheckCircle2 size={16} className="shrink-0" />}
                    {notice.text}
                  </div>
                )}

                {plans.map((plan) => {
                  const popular = plan.id === "pro";
                  return (
                    <div
                      key={plan.id}
                      className={`rounded-xl border p-4 relative ${popular ? "border-indigo-500" : "border-white/10"}`}
                    >
                      {popular && (
                        <span className="absolute right-3 top-3 text-xs bg-indigo-600 px-2 py-1 rounded-full text-white">
                          Best value
                        </span>
                      )}
                      <h3 className="text-white font-semibold flex items-center gap-2">
                        {plan.name}
                        {popular && <Zap size={16} className="text-yellow-400" />}
                      </h3>
                      <p className="text-indigo-400 text-2xl font-bold mt-2">₹{plan.amount}</p>
                      <p className="text-slate-400 text-sm mt-1">
                        {plan.credits} credits · valid {plan.validityDays} days
                      </p>
                      <button
                        disabled={!!busy}
                        className="mt-4 w-full rounded-lg bg-indigo-600 hover:bg-indigo-700 py-2 text-white cursor-pointer disabled:opacity-60"
                        onClick={() => handleUpgrade(plan.id)}
                      >
                        {busy === plan.id ? "Opening checkout…" : "Buy credits"}
                      </button>
                    </div>
                  );
                })}

                <div className="rounded-xl border border-white/10 p-4">
                  <p className="text-slate-300 text-sm font-medium mb-2">Credit cost per request</p>
                  <ul className="text-[12.5px] text-slate-400 space-y-1">
                    {COSTS.map(([label, cost]) => (
                      <li key={label} className="flex justify-between">
                        <span>{label}</span>
                        <span className="text-slate-300">{cost}</span>
                      </li>
                    ))}
                  </ul>
                  <p className="text-[11px] text-slate-600 mt-3">Failed requests are refunded automatically.</p>
                </div>
              </div>
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}
