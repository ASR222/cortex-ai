import { useState } from "react";
import { useDispatch, useSelector } from "react-redux";
import { FaGoogle } from "react-icons/fa";
import { signInWithPopup } from "firebase/auth";
import ArtifactPanel from "../components/ArtifactPanel";
import ChatArea from "../components/ChatArea";
import Mark from "../components/Mark";
import Sidebar from "../components/Sidebar";
import { setUserData } from "../redux/user.slice";
import { login } from "../features/user.api";
import { auth, googleProvider } from "../../firebase";

function Home() {
  const { userData, checked } = useSelector((state) => state.user);
  const dispatch = useDispatch();
  const [signingIn, setSigningIn] = useState(false);
  const [error, setError] = useState("");

  const handleGoogleLogin = async () => {
    setError("");
    setSigningIn(true);
    try {
      const result = await signInWithPopup(auth, googleProvider);
      const idToken = await result.user.getIdToken();
      dispatch(setUserData(await login(idToken)));
    } catch (err) {
      if (err?.code !== "auth/popup-closed-by-user") {
        setError(err?.response?.data?.message || "Sign-in failed. Please try again.");
      }
    } finally {
      setSigningIn(false);
    }
  };

  return (
    <div className="h-dvh flex bg-ground text-ink overflow-hidden">
      <Sidebar />
      <ChatArea />
      <ArtifactPanel />

      {checked && !userData && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 px-4">
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="signin-title"
            className="w-full max-w-[380px] bg-surface rounded-[20px] p-7 flex flex-col gap-6"
          >
            <div className="flex flex-col gap-3">
              <Mark size={28} />
              <h2 id="signin-title" className="text-[20px] font-semibold tracking-[-0.01em]">
                Sign in to CortexAI
              </h2>
              <p className="text-[14px] leading-[22px] text-muted">
                One chat box, eight specialists. Each message goes to the one that fits: chat, web search, coding,
                questions about a PDF or image, or a new PDF, slide deck or image.
              </p>
            </div>

            <div className="flex flex-col gap-3">
              <button
                onClick={handleGoogleLogin}
                disabled={signingIn}
                className="w-full h-11 flex items-center justify-center gap-2.5 rounded-full text-[14px] font-medium bg-ink text-ground hover:bg-soft active:scale-[0.99] transition cursor-pointer disabled:opacity-60 disabled:cursor-wait"
              >
                <FaGoogle size={14} />
                {signingIn ? "Signing in…" : "Continue with Google"}
              </button>
              {error && (
                <p role="alert" className="text-[13px] text-negative">
                  {error}
                </p>
              )}
              <p className="text-[12px] text-faint">New accounts start with free credits. No card needed.</p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default Home;
