import Home from "./pages/Home";
import useCurrentUser from "./hooks/useCurrentUser";

function App() {
  useCurrentUser();
  return <Home />;
}

export default App;
