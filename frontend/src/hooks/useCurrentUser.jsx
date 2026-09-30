import { useEffect } from "react";
import { useDispatch } from "react-redux";
import { getMe } from "../features/user.api";
import { setUserData } from "../redux/user.slice";
import { setUnauthorizedHandler } from "../utils/axios";

/** Loads the signed-in user on startup and logs out locally on any 401. */
function useCurrentUser() {
  const dispatch = useDispatch();

  useEffect(() => {
    setUnauthorizedHandler(() => dispatch(setUserData(null)));
    getMe()
      .then((user) => dispatch(setUserData(user)))
      .catch(() => dispatch(setUserData(null)));
  }, [dispatch]);
}

export default useCurrentUser;
