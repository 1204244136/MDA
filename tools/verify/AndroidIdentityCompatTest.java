import com.aliothmoon.maafw.membership.AndroidIdentityCompat;
import java.lang.reflect.InvocationTargetException;
import java.util.Map;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeoutException;

/** 直接执行正式会员兼容层，校验每轮快照刷新与错误脱敏。 */
public class AndroidIdentityCompatTest {
    private static void check(boolean ok) { if (!ok) throw new AssertionError(); }

    public static void main(String[] args) {
        check(!AndroidIdentityCompat.refreshRequired(Map.of()));
        check(!AndroidIdentityCompat.refreshRequired(Map.of("MDA_ANDROID_IDENTITY_APP_JSON", "")));
        check(AndroidIdentityCompat.refreshRequired(Map.of("MDA_ANDROID_IDENTITY_APP_JSON", "fresh")));
        Throwable denied = new ExecutionException(new InvocationTargetException(new SecurityException("PRIVATE-SOURCE")));
        check("permission_denied:SecurityException".equals(AndroidIdentityCompat.errorStatus(denied)));
        check(!AndroidIdentityCompat.diagnostic(denied).toString().contains("PRIVATE-SOURCE"));
        check("timeout:TimeoutException".equals(AndroidIdentityCompat.errorStatus(new TimeoutException())));
        check("unsupported:NoSuchMethodException".equals(AndroidIdentityCompat.errorStatus(new NoSuchMethodException())));
        System.out.println("AndroidIdentityCompat: fresh snapshot and safe diagnostics PASS");
    }
}
