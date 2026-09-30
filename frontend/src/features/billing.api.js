import api from "../utils/axios";

export const getPlans = async () => {
  const { data } = await api.get("/api/billing/plans");
  return data;
};

export const createOrder = async (plan) => {
  const { data } = await api.post("/api/billing/orders", { plan });
  return data;
};

export const verifyPayment = async (payload) => {
  const { data } = await api.post("/api/billing/verify", payload);
  return data;
};

/** Loads Razorpay Checkout on demand instead of on every page view. */
export const loadRazorpay = () =>
  new Promise((resolve, reject) => {
    if (window.Razorpay) return resolve(window.Razorpay);
    const script = document.createElement("script");
    script.src = "https://checkout.razorpay.com/v1/checkout.js";
    script.onload = () => resolve(window.Razorpay);
    script.onerror = () => reject(new Error("Could not load Razorpay"));
    document.body.appendChild(script);
  });
