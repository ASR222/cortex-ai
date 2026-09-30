import { useState } from "react";
import { useDispatch, useSelector } from "react-redux";
import { FaGoogle } from "react-icons/fa";
import { signInWithPopup } from "firebase/auth";
import ArtifactPanel from "../components/ArtifactPanel";
import ChatArea from "../components/ChatArea";
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
    <div className="h-screen flex bg-[#0d0f14] text-white overflow-hidden">
      <Sidebar />
      <ChatArea />
      <ArtifactPanel />

      {checked && !userData && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm px-4">
          <div className="w-full max-w-[360px] bg-[#13151c] border border-white/[0.08] rounded-2xl p-7 flex flex-col gap-5">
            <div className="flex flex-col gap-1.5">
              <h2 className="text-[17px] font-semibold text-slate-100 tracking-tight">Welcome to CortexAI</h2>
              <p className="text-[13px] text-slate-500 leading-relaxed">
                A multi-agent assistant that chats, searches the web, writes code, reads your PDFs and creates
                documents, slides and images.
              </p>
            </div>

            <button
              onClick={handleGoogleLogin}
              disabled={signingIn}
              className="w-full flex items-center justify-center gap-3 py-[11px] rounded-xl text-sm font-medium text-white bg-gradient-to-br from-indigo-500 to-violet-700 hover:from-indigo-400 hover:to-violet-600 border border-indigo-500/30 shadow-lg shadow-indigo-500/20 transition-all duration-150 cursor-pointer disabled:opacity-60"
            >
              <FaGoogle size={15} className="text-white" />
              {signingIn ? "Signing in…" : "Continue with Google"}
            </button>

            {error && <p className="text-[12px] text-red-400">{error}</p>}
            <p className="text-[11px] text-slate-600">New accounts start with free credits. No card needed.</p>
          </div>
        </div>
      )}
    </div>
  );
}

export default Home;
