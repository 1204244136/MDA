package com.aliothmoon.maafw.membership;

import java.lang.reflect.InvocationTargetException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeoutException;

/** 不依赖 Android 的签名兼容与安全错误诊断，供 JVM 回归直接执行。 */
public final class AndroidIdentityCompat {
    private AndroidIdentityCompat() {}

    public static boolean refreshRequired(Map<String, String> env) {
        String app = env.get("MDA_ANDROID_IDENTITY_APP_JSON");
        return app != null && !app.isEmpty();
    }

    public static final class CallFailure extends Exception {
        public final String stage;
        public CallFailure(String stage, Throwable cause) {
            super(null, cause);
            this.stage = stage;
        }
    }

    private static Throwable root(Throwable error) {
        while (error instanceof InvocationTargetException || error instanceof ExecutionException
                || error instanceof CallFailure) {
            if (error.getCause() == null) break;
            error = error.getCause();
        }
        return error;
    }

    public static String errorStatus(Throwable error) {
        Throwable cause = root(error);
        String kind = cause instanceof SecurityException ? "permission_denied"
                : cause instanceof TimeoutException ? "timeout"
                : cause instanceof UnsupportedOperationException || cause instanceof NoSuchMethodException
                    || cause instanceof ClassNotFoundException ? "unsupported" : "read_error";
        return kind + ":" + cause.getClass().getSimpleName();
    }

    /** 不写异常 message/toString/参数，只保留阶段与最多六个代码位置。 */
    public static Map<String, Object> diagnostic(Throwable error) {
        Map<String, Object> result = new LinkedHashMap<>();
        Throwable cursor = error;
        while (cursor != null) {
            if (cursor instanceof CallFailure) {
                result.put("stage", ((CallFailure) cursor).stage);
                break;
            }
            cursor = cursor.getCause();
        }
        List<String> frames = new ArrayList<>();
        for (StackTraceElement frame : root(error).getStackTrace()) {
            frames.add(frame.getClassName() + "#" + frame.getMethodName() + ":" + frame.getLineNumber());
            if (frames.size() == 6) break;
        }
        result.put("frames", frames);
        return result;
    }
}
