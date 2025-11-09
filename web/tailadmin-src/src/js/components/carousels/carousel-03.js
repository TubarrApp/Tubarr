// import Swiper JS
import Swiper from "swiper";
import { Pagination, Autoplay } from "swiper/modules";
// import Swiper styles
import "swiper/css";
import "swiper/css/pagination";

const swiper = new Swiper(".carouselThree", {
  modules: [Pagination, Autoplay],
  autoplay: {
    delay: 5000,
    disableOnInteraction: false,
  },
  pagination: {
    el: ".swiper-pagination",
    clickable: true,
  },
});
